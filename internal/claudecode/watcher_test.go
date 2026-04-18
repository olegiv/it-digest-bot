package claudecode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestWatcherPostsNewRelease(t *testing.T) {
	t.Parallel()

	npmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"dist-tags":{"latest":"2.1.114"},"time":{"2.1.114":"2025-04-18T10:00:00Z"}}`)
	}))
	defer npmSrv.Close()

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"html_url":"https://github.com/x/y/releases/tag/v2.1.114","body":"- new stuff","tag_name":"v2.1.114"}`)
	}))
	defer ghAPI.Close()

	var tgBody string
	var tgCalls int32
	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tgCalls, 1)
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		tgBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5150}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	w := &Watcher{
		Package:    "@anthropic-ai/claude-code",
		GitHubRepo: "anthropics/claude-code",
		Channel:    "@ch",
		NPM:        NewNPMClient(testHTTP()).WithBaseURL(npmSrv.URL),
		GitHub:     NewGitHubClient(testHTTP(), "").WithBaseURLs(ghAPI.URL, "unused"),
		Bot:        telegram.New("tg-token", telegram.WithBaseURL(tgSrv.URL), telegram.WithHTTPClient(testHTTP())),
		Releases:   st.Releases,
		Posts:      st.Posts,
	}

	res, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Posted {
		t.Error("expected Posted=true")
	}
	if res.MessageID != 5150 {
		t.Errorf("MessageID = %d", res.MessageID)
	}
	if atomic.LoadInt32(&tgCalls) != 1 {
		t.Errorf("telegram calls = %d", atomic.LoadInt32(&tgCalls))
	}
	if !strings.Contains(tgBody, "new stuff") {
		t.Errorf("telegram body missing release notes: %s", tgBody)
	}

	// Second run should be a no-op.
	res2, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if res2.Posted {
		t.Error("second run should not post")
	}
	if atomic.LoadInt32(&tgCalls) != 1 {
		t.Errorf("telegram called on no-op: %d", atomic.LoadInt32(&tgCalls))
	}

	rel, err := st.Releases.GetLatestSeen(context.Background(), "@anthropic-ai/claude-code")
	if err != nil {
		t.Fatalf("GetLatestSeen: %v", err)
	}
	if rel.Version != "2.1.114" {
		t.Errorf("recorded version = %q", rel.Version)
	}

	n, _ := st.Posts.Count(context.Background(), store.KindRelease)
	if n != 1 {
		t.Errorf("posts_log rows = %d", n)
	}
}

func TestWatcherUsesChangelogFallback(t *testing.T) {
	t.Parallel()

	npmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"dist-tags":{"latest":"2.1.114"},"time":{"2.1.114":"2025-04-18T10:00:00Z"}}`)
	}))
	defer npmSrv.Close()

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ghAPI.Close()

	ghRaw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "## 2.1.114\n- fallback note\n")
	}))
	defer ghRaw.Close()

	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	w := &Watcher{
		Package:    "@anthropic-ai/claude-code",
		GitHubRepo: "anthropics/claude-code",
		Channel:    "@ch",
		NPM:        NewNPMClient(testHTTP()).WithBaseURL(npmSrv.URL),
		GitHub:     NewGitHubClient(testHTTP(), "").WithBaseURLs(ghAPI.URL, ghRaw.URL),
		Bot:        telegram.New("t", telegram.WithBaseURL(tgSrv.URL), telegram.WithHTTPClient(testHTTP())),
		Releases:   st.Releases,
		Posts:      st.Posts,
	}

	if _, err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

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

// TestWatcherSkipsAlreadySeenAfterDistTagRollback reproduces the case
// where releases_seen contains both the npm-current version V (older
// posted_at) and a newer version V2 (more recent posted_at) — e.g. when
// V2 was published, posted by us, and then npm's `latest` dist-tag was
// rolled back to V (yanked release / dist-tag retraction). The watcher
// must NOT re-post V: HasSeen looks up by (package, version), so the
// fact that V2 is the most-recent row is irrelevant.
func TestWatcherSkipsAlreadySeenAfterDistTagRollback(t *testing.T) {
	t.Parallel()

	var ghCalls, tgCalls int32
	npmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"dist-tags":{"latest":"2.1.119"},"time":{"2.1.119":"2026-04-24T00:00:00Z"}}`)
	}))
	defer npmSrv.Close()

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ghCalls, 1)
		http.NotFound(w, r)
	}))
	defer ghAPI.Close()

	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&tgCalls, 1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":99}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	pkg := "@anthropic-ai/claude-code"

	_, err := st.DB().ExecContext(context.Background(), `
        INSERT INTO releases_seen (package, version, posted_at, tg_message_id)
        VALUES (?, ?, ?, ?), (?, ?, ?, ?)`,
		pkg, "2.1.119", "2026-04-24 00:15:00", 1,
		pkg, "2.1.120", "2026-04-25 00:15:00", 2)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	w := &Watcher{
		Package:    pkg,
		GitHubRepo: "anthropics/claude-code",
		Channel:    "@ch",
		NPM:        NewNPMClient(testHTTP()).WithBaseURL(npmSrv.URL),
		GitHub:     NewGitHubClient(testHTTP(), "").WithBaseURLs(ghAPI.URL, "unused"),
		Bot:        telegram.New("t", telegram.WithBaseURL(tgSrv.URL), telegram.WithHTTPClient(testHTTP())),
		Releases:   st.Releases,
		Posts:      st.Posts,
	}

	res, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Posted {
		t.Error("expected Posted=false: 2.1.119 is already in releases_seen")
	}
	if got := atomic.LoadInt32(&tgCalls); got != 0 {
		t.Errorf("telegram calls = %d, want 0", got)
	}
	if got := atomic.LoadInt32(&ghCalls); got != 0 {
		t.Errorf("github calls = %d, want 0 (skip path must not fetch notes)", got)
	}
}

// TestWatcherSkipsWhenNpmAndGitHubDisagree exercises the agreement
// guard introduced after the 2026-04-25 incident: npm published
// 2.1.120 as `latest`, then rolled back to 2.1.119, but GitHub never
// promoted 2.1.120 to its "Latest release" marker. The bot must defer
// posting until both signals agree.
func TestWatcherSkipsWhenNpmAndGitHubDisagree(t *testing.T) {
	t.Parallel()

	var tgCalls int32
	npmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"dist-tags":{"latest":"2.1.120"},"time":{"2.1.120":"2026-04-25T00:00:00Z"}}`)
	}))
	defer npmSrv.Close()

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("watcher hit unexpected GitHub path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"html_url":"https://github.com/anthropics/claude-code/releases/tag/v2.1.119","body":"- old","tag_name":"v2.1.119"}`)
	}))
	defer ghAPI.Close()

	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&tgCalls, 1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	w := &Watcher{
		Package:    "@anthropic-ai/claude-code",
		GitHubRepo: "anthropics/claude-code",
		Channel:    "@ch",
		NPM:        NewNPMClient(testHTTP()).WithBaseURL(npmSrv.URL),
		GitHub:     NewGitHubClient(testHTTP(), "").WithBaseURLs(ghAPI.URL, "unused"),
		Bot:        telegram.New("t", telegram.WithBaseURL(tgSrv.URL), telegram.WithHTTPClient(testHTTP())),
		Releases:   st.Releases,
		Posts:      st.Posts,
	}

	res, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Posted {
		t.Error("expected Posted=false on disagreement")
	}
	if got := atomic.LoadInt32(&tgCalls); got != 0 {
		t.Errorf("telegram calls = %d, want 0", got)
	}
	has, err := st.Releases.HasSeen(context.Background(), "@anthropic-ai/claude-code", "2.1.120")
	if err != nil {
		t.Fatalf("HasSeen: %v", err)
	}
	if has {
		t.Error("releases_seen must not contain a row for the disagreed version")
	}
}

// TestWatcherSkipsWhenGitHubLatestNotFound covers the case where
// GitHub returns 404 for /releases/latest (e.g. the most recent
// release is a draft or prerelease, or none exist). The watcher
// must defer cleanly without erroring out.
func TestWatcherSkipsWhenGitHubLatestNotFound(t *testing.T) {
	t.Parallel()

	var tgCalls int32
	npmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"dist-tags":{"latest":"2.1.120"},"time":{"2.1.120":"2026-04-25T00:00:00Z"}}`)
	}))
	defer npmSrv.Close()

	ghAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ghAPI.Close()

	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&tgCalls, 1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	w := &Watcher{
		Package:    "@anthropic-ai/claude-code",
		GitHubRepo: "anthropics/claude-code",
		Channel:    "@ch",
		NPM:        NewNPMClient(testHTTP()).WithBaseURL(npmSrv.URL),
		GitHub:     NewGitHubClient(testHTTP(), "").WithBaseURLs(ghAPI.URL, "unused"),
		Bot:        telegram.New("t", telegram.WithBaseURL(tgSrv.URL), telegram.WithHTTPClient(testHTTP())),
		Releases:   st.Releases,
		Posts:      st.Posts,
	}

	res, err := w.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Posted {
		t.Error("expected Posted=false when GitHub has no latest release")
	}
	if got := atomic.LoadInt32(&tgCalls); got != 0 {
		t.Errorf("telegram calls = %d, want 0", got)
	}
}

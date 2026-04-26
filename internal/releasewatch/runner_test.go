package releasewatch

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
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

type fakeSource struct {
	name       string
	candidates []Candidate
	err        error
}

func (s fakeSource) Name() string { return s.name }

func (s fakeSource) Candidates(context.Context) ([]Candidate, error) {
	return s.candidates, s.err
}

type fakeSender struct {
	calls    int
	messages []string
	err      error
}

func (s *fakeSender) SendMessage(_ context.Context, _ string, text string, mode telegram.ParseMode) (int64, error) {
	s.calls++
	s.messages = append(s.messages, text)
	if mode != telegram.ParseModeMarkdownV2 {
		return 0, errors.New("unexpected parse mode")
	}
	if s.err != nil {
		return 0, s.err
	}
	return int64(100 + s.calls), nil
}

func TestRunnerPostsAndRecordsMultipleSources(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	r := &Runner{
		Sources: []Source{
			fakeSource{name: "one", candidates: []Candidate{candidate("one", "pkg-one", "1.0.0")}},
			fakeSource{name: "two", candidates: []Candidate{candidate("two", "pkg-two", "2.0.0")}},
		},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
	}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PostedCount() != 2 {
		t.Fatalf("posted count = %d, want 2", res.PostedCount())
	}
	if bot.calls != 2 {
		t.Fatalf("telegram calls = %d, want 2", bot.calls)
	}
	for _, tc := range []struct {
		pkg, version string
	}{
		{"pkg-one", "1.0.0"},
		{"pkg-two", "2.0.0"},
	} {
		seen, err := st.Releases.HasSeen(context.Background(), tc.pkg, tc.version)
		if err != nil {
			t.Fatalf("HasSeen: %v", err)
		}
		if !seen {
			t.Errorf("%s %s was not recorded", tc.pkg, tc.version)
		}
	}
	n, err := st.Posts.Count(context.Background(), store.KindRelease)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Errorf("posts_log rows = %d, want 2", n)
	}
}

func TestRunnerSkipsSeenBeforeRender(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	if err := st.Releases.RecordSeen(context.Background(), "pkg", "1.0.0", 42, "https://example.com/old"); err != nil {
		t.Fatalf("RecordSeen: %v", err)
	}
	rendered := false
	cand := Candidate{
		Source:  "src",
		Package: "pkg",
		Version: "1.0.0",
		Render: func(context.Context) (*Announcement, error) {
			rendered = true
			return announcement("pkg", "1.0.0"), nil
		},
	}
	bot := &fakeSender{}
	r := &Runner{
		Sources:  []Source{fakeSource{name: "src", candidates: []Candidate{cand}}},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
	}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Items) != 1 || !res.Items[0].Seen {
		t.Fatalf("result item = %+v, want seen", res.Items)
	}
	if rendered {
		t.Error("seen candidate was rendered")
	}
	if bot.calls != 0 {
		t.Errorf("telegram calls = %d, want 0", bot.calls)
	}
}

func TestRunnerDryRunSkipsWrites(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	var out bytes.Buffer
	r := &Runner{
		Sources:  []Source{fakeSource{name: "src", candidates: []Candidate{candidate("src", "pkg", "1.0.0")}}},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
		DryRun:   true,
		DryOut:   &out,
	}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PostedCount() != 0 {
		t.Errorf("posted count = %d, want 0", res.PostedCount())
	}
	if bot.calls != 0 {
		t.Errorf("telegram calls = %d, want 0", bot.calls)
	}
	seen, err := st.Releases.HasSeen(context.Background(), "pkg", "1.0.0")
	if err != nil {
		t.Fatalf("HasSeen: %v", err)
	}
	if seen {
		t.Error("dry-run recorded release")
	}
	if !strings.Contains(out.String(), "RELEASE pkg 1.0.0") {
		t.Errorf("dry-run output missing release header:\n%s", out.String())
	}
}

func TestRunnerDefersCleanly(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	cand := Candidate{
		Source:  "src",
		Package: "pkg",
		Version: "1.0.0",
		Render: func(context.Context) (*Announcement, error) {
			return nil, Deferf("upstream signals disagree")
		},
	}
	bot := &fakeSender{}
	r := &Runner{
		Sources:  []Source{fakeSource{name: "src", candidates: []Candidate{cand}}},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
	}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Items) != 1 || !res.Items[0].Deferred {
		t.Fatalf("result item = %+v, want deferred", res.Items)
	}
	if bot.calls != 0 {
		t.Errorf("telegram calls = %d, want 0", bot.calls)
	}
}

func TestRunnerContinuesAfterSourceError(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	r := &Runner{
		Sources: []Source{
			fakeSource{name: "broken", err: errors.New("boom")},
			fakeSource{name: "healthy", candidates: []Candidate{candidate("healthy", "pkg", "1.0.0")}},
		},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
	}

	res, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected source error")
	}
	if !strings.Contains(err.Error(), "source broken candidates: boom") {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PostedCount() != 1 {
		t.Fatalf("posted count = %d, want 1", res.PostedCount())
	}
	if bot.calls != 1 {
		t.Fatalf("telegram calls = %d, want 1", bot.calls)
	}
	seen, err := st.Releases.HasSeen(context.Background(), "pkg", "1.0.0")
	if err != nil {
		t.Fatalf("HasSeen: %v", err)
	}
	if !seen {
		t.Error("healthy source release was not recorded")
	}
}

func TestRunnerContinuesAfterCandidateError(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	broken := Candidate{
		Source:  "broken",
		Package: "pkg-broken",
		Version: "1.0.0",
		Render: func(context.Context) (*Announcement, error) {
			return nil, errors.New("render boom")
		},
	}
	r := &Runner{
		Sources: []Source{
			fakeSource{name: "broken", candidates: []Candidate{broken}},
			fakeSource{name: "healthy", candidates: []Candidate{candidate("healthy", "pkg", "1.0.0")}},
		},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
	}

	res, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected candidate error")
	}
	if !strings.Contains(err.Error(), "render release pkg-broken 1.0.0: render boom") {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PostedCount() != 1 {
		t.Fatalf("posted count = %d, want 1", res.PostedCount())
	}
	if bot.calls != 1 {
		t.Fatalf("telegram calls = %d, want 1", bot.calls)
	}
	seen, err := st.Releases.HasSeen(context.Background(), "pkg", "1.0.0")
	if err != nil {
		t.Fatalf("HasSeen: %v", err)
	}
	if !seen {
		t.Error("healthy source release was not recorded")
	}
}

func TestRunnerReturnsSourceErrors(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	r := &Runner{
		Sources:  []Source{fakeSource{name: "broken", err: errors.New("boom")}},
		Channel:  "@ch",
		Bot:      &fakeSender{},
		Releases: st.Releases,
		Posts:    st.Posts,
	}

	_, err := r.Run(context.Background())
	if err == nil {
		t.Fatal("expected source error")
	}
	if !strings.Contains(err.Error(), "source broken candidates: boom") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func candidate(source, pkg, version string) Candidate {
	return Candidate{
		Source:  source,
		Package: pkg,
		Version: version,
		Render: func(context.Context) (*Announcement, error) {
			return announcement(pkg, version), nil
		},
	}
}

func announcement(pkg, version string) *Announcement {
	return &Announcement{
		Text:       "*" + pkg + "* `" + version + "`",
		ReleaseURL: "https://example.com/" + version,
		Payload: map[string]any{
			"package": pkg,
			"version": version,
		},
	}
}

package releasewatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	onSend   func() // optional hook, run before the result is decided
}

func (s *fakeSender) SendMessage(_ context.Context, _ string, text string, mode telegram.ParseMode) (int64, error) {
	s.calls++
	s.messages = append(s.messages, text)
	if s.onSend != nil {
		s.onSend()
	}
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
	if len(res.Items) != 1 || res.Items[0].Outcome != OutcomeSeen {
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
	if len(res.Items) != 1 || res.Items[0].Outcome != OutcomeDeferred {
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

// fakeSeeder is a Source that also implements Seeder.
type fakeSeeder struct {
	fakeSource
	pkg       string
	notice    *Announcement
	noticeErr error
	noticed   int
}

func (s *fakeSeeder) SeedPackage() string { return s.pkg }

func (s *fakeSeeder) SeedNotice(_ context.Context, cands []Candidate) (*Announcement, error) {
	s.noticed = len(cands)
	return s.notice, s.noticeErr
}

func seedCandidates(source, pkg string, versions ...string) []Candidate {
	out := make([]Candidate, 0, len(versions))
	for _, v := range versions {
		c := candidate(source, pkg, v)
		c.URL = "https://example.com/" + v
		out = append(out, c)
	}
	return out
}

func TestRunnerSeedsHistoryOnFirstRun(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1", "2", "3")},
		pkg:        "pkg-feed",
		notice:     &Announcement{Text: "*feed* now tracked"},
	}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PostedCount() != 0 || res.SeededCount() != 3 || len(res.Items) != 3 {
		t.Fatalf("result = posted %d seeded %d items %d", res.PostedCount(), res.SeededCount(), len(res.Items))
	}
	if bot.calls != 1 || bot.messages[0] != "*feed* now tracked" {
		t.Fatalf("telegram calls = %d messages = %v, want the single seed notice", bot.calls, bot.messages)
	}
	if src.noticed != 3 {
		t.Errorf("SeedNotice saw %d candidates, want 3", src.noticed)
	}
	for _, v := range []string{"1", "2", "3"} {
		seen, err := st.Releases.HasSeen(context.Background(), "pkg-feed", v)
		if err != nil || !seen {
			t.Errorf("version %s not recorded as seen (%v)", v, err)
		}
	}
	latest, err := st.Releases.GetLatestSeen(context.Background(), "pkg-feed")
	if err != nil {
		t.Fatalf("GetLatestSeen: %v", err)
	}
	if !latest.ReleaseURL.Valid || !strings.HasPrefix(latest.ReleaseURL.String, "https://example.com/") {
		t.Errorf("seeded row lost candidate URL: %+v", latest.ReleaseURL)
	}
	if !latest.TgMessageID.Valid || latest.TgMessageID.Int64 != 101 {
		t.Errorf("seeded row message id = %+v, want notice id 101", latest.TgMessageID)
	}
	if n, _ := st.Posts.Count(context.Background(), store.KindSeed); n != 1 {
		t.Errorf("seed posts_log rows = %d, want 1", n)
	}
	if n, _ := st.Posts.Count(context.Background(), store.KindRelease); n != 0 {
		t.Errorf("release posts_log rows = %d, want 0", n)
	}

	// Second run: one new candidate appears; only it is posted.
	src.candidates = seedCandidates("feed", "pkg-feed", "1", "2", "3", "4")
	res, err = r.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.PostedCount() != 1 || res.SeededCount() != 0 {
		t.Fatalf("second run posted %d seeded %d", res.PostedCount(), res.SeededCount())
	}
	if bot.calls != 2 || !strings.Contains(bot.messages[1], "`4`") {
		t.Fatalf("second run messages = %v", bot.messages)
	}
	seenCount := 0
	for _, item := range res.Items {
		if item.Outcome == OutcomeSeen {
			seenCount++
		}
	}
	if seenCount != 3 {
		t.Errorf("seen items = %d, want 3", seenCount)
	}
}

func TestRunnerSeedSilentlyWhenNoticeIsNil(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1", "2")},
		pkg:        "pkg-feed",
	}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if bot.calls != 0 {
		t.Errorf("silent seed sent %d messages", bot.calls)
	}
	if res.SeededCount() != 2 {
		t.Errorf("seeded = %d, want 2", res.SeededCount())
	}
	latest, err := st.Releases.GetLatestSeen(context.Background(), "pkg-feed")
	if err != nil {
		t.Fatalf("GetLatestSeen: %v", err)
	}
	if latest.TgMessageID.Valid {
		t.Errorf("silent seed should store NULL message id, got %+v", latest.TgMessageID)
	}
}

func TestRunnerSeedDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	var out bytes.Buffer
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1", "2")},
		pkg:        "pkg-feed",
		notice:     &Announcement{Text: "notice text"},
	}
	r := &Runner{
		Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts,
		DryRun: true, DryOut: &out,
	}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if bot.calls != 0 {
		t.Errorf("dry-run seed sent %d messages", bot.calls)
	}
	if res.SeededCount() != 2 || res.PostedCount() != 0 {
		t.Errorf("result = %+v", res.Items)
	}
	if _, err := st.Releases.GetLatestSeen(context.Background(), "pkg-feed"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("dry-run seed wrote releases_seen rows: %v", err)
	}
	if n, _ := st.Posts.Count(context.Background(), store.KindSeed); n != 0 {
		t.Errorf("dry-run seed wrote posts_log rows: %d", n)
	}
	text := out.String()
	for _, want := range []string{"SEED feed - would record 2 candidates", "pkg-feed 1", "pkg-feed 2", "SEED NOTICE - 11 bytes", "notice text", "END DRY-RUN"} {
		if !strings.Contains(text, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, text)
		}
	}
}

func TestRunnerSilentSeedDryRunOutput(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	var out bytes.Buffer
	src := &fakeSeeder{fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1")}, pkg: "pkg-feed"}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: &fakeSender{}, Releases: st.Releases, Posts: st.Posts, DryRun: true, DryOut: &out}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "SEED NOTICE - none (silent seed)") {
		t.Errorf("dry-run output for a silent seed:\n%s", out.String())
	}
}

func TestRunnerSeedNoticeSendFailureRecordsNothing(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{err: errors.New("telegram down")}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1", "2")},
		pkg:        "pkg-feed",
		notice:     &Announcement{Text: "notice"},
	}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "source feed seed: telegram send seed notice: telegram down") {
		t.Fatalf("expected seed send error, got %v", err)
	}
	if res.SeededCount() != 0 || len(res.Items) != 0 {
		t.Errorf("failed seed produced items: %+v", res.Items)
	}
	if _, err := st.Releases.GetLatestSeen(context.Background(), "pkg-feed"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("failed seed wrote releases_seen rows: %v", err)
	}
	// Next run (Telegram back) seeds normally instead of posting history.
	bot.err = nil
	res, err = r.Run(context.Background())
	if err != nil {
		t.Fatalf("retry Run: %v", err)
	}
	if res.SeededCount() != 2 || res.PostedCount() != 0 || bot.calls != 2 {
		t.Errorf("retry seeded %d posted %d calls %d", res.SeededCount(), res.PostedCount(), bot.calls)
	}
}

func TestRunnerSeedRecordFailureLeavesNoHistory(t *testing.T) {
	t.Parallel()

	// The notice goes out, then the store write fails (simulated by
	// cancelling the run context from inside the send). Nothing may be
	// recorded: a partial history would turn the next run into a flood of
	// individual posts for every item the seed did not reach.
	st := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bot := &fakeSender{onSend: cancel}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1", "2", "3")},
		pkg:        "pkg-feed",
		notice:     &Announcement{Text: "notice"},
	}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "record seeded history pkg-feed (notice message_id=101 already posted):") {
		t.Fatalf("expected seed record error, got %v", err)
	}
	if bot.calls != 1 {
		t.Errorf("telegram calls = %d, want the single notice", bot.calls)
	}
	if res.SeededCount() != 0 || len(res.Items) != 0 {
		t.Errorf("failed seed produced items: %+v", res.Items)
	}
	if _, err := st.Releases.GetLatestSeen(context.Background(), "pkg-feed"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("failed seed left releases_seen rows behind: %v", err)
	}

	// Next run seeds again (one notice, no individual posts) instead of
	// announcing history.
	bot.onSend = nil
	res, err = r.Run(context.Background())
	if err != nil {
		t.Fatalf("retry Run: %v", err)
	}
	if res.SeededCount() != 3 || res.PostedCount() != 0 || bot.calls != 2 {
		t.Errorf("retry seeded %d posted %d calls %d, want 3/0/2", res.SeededCount(), res.PostedCount(), bot.calls)
	}
}

func TestRunnerSeedNoticeErrorSkipsSource(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	bot := &fakeSender{}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1")},
		pkg:        "pkg-feed",
		noticeErr:  errors.New("render boom"),
	}
	healthy := fakeSource{name: "healthy", candidates: []Candidate{candidate("healthy", "pkg", "1.0.0")}}
	r := &Runner{Sources: []Source{src, healthy}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "render seed notice: render boom") {
		t.Fatalf("expected seed notice error, got %v", err)
	}
	if res.PostedCount() != 1 || bot.calls != 1 {
		t.Errorf("healthy source should still post: posted %d calls %d", res.PostedCount(), bot.calls)
	}
	if seen, _ := st.Releases.HasSeen(context.Background(), "pkg-feed", "1"); seen {
		t.Error("failed seed must not record history")
	}
}

func TestRunnerSeederWithEmptyPackageIsAnError(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	src := &fakeSeeder{fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1")}}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: &fakeSender{}, Releases: st.Releases, Posts: st.Posts}
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "seed package is required") {
		t.Fatalf("expected seed package error, got %v", err)
	}
}

func TestRunnerCapsPostsPerRun(t *testing.T) {
	t.Parallel()

	// Two plain sources with 7 unseen candidates each: the default cap of 10
	// applies across sources, the remaining 4 are left unseen (not recorded)
	// and posted on the next run.
	st := openStore(t)
	bot := &fakeSender{}
	r := &Runner{
		Sources: []Source{
			fakeSource{name: "a", candidates: seedCandidates("a", "pkg-a", "1", "2", "3", "4", "5", "6", "7")},
			fakeSource{name: "b", candidates: seedCandidates("b", "pkg-b", "1", "2", "3", "4", "5", "6", "7")},
		},
		Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts,
	}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PostedCount() != DefaultMaxPostsPerRun || res.CappedCount() != 4 || bot.calls != DefaultMaxPostsPerRun {
		t.Fatalf("posted %d capped %d calls %d, want 10/4/10", res.PostedCount(), res.CappedCount(), bot.calls)
	}
	// Source order is preserved: all of a, then the first three of b.
	for _, v := range []string{"5", "6", "7"} {
		if seen, _ := st.Releases.HasSeen(context.Background(), "pkg-b", v); seen {
			t.Errorf("capped pkg-b %s must not be recorded", v)
		}
	}
	if seen, _ := st.Releases.HasSeen(context.Background(), "pkg-b", "3"); !seen {
		t.Error("pkg-b 3 should have been posted within the cap")
	}

	res, err = r.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.PostedCount() != 4 || res.CappedCount() != 0 || bot.calls != 14 {
		t.Errorf("second run posted %d capped %d calls %d, want 4/0/14", res.PostedCount(), res.CappedCount(), bot.calls)
	}
}

func TestRunnerPostCapOverrides(t *testing.T) {
	t.Parallel()

	cands := seedCandidates("a", "pkg", "1", "2", "3")

	t.Run("explicit limit", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		bot := &fakeSender{}
		r := &Runner{Sources: []Source{fakeSource{name: "a", candidates: cands}}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts, MaxPostsPerRun: 2}
		res, err := r.Run(context.Background())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.PostedCount() != 2 || res.CappedCount() != 1 {
			t.Errorf("posted %d capped %d, want 2/1", res.PostedCount(), res.CappedCount())
		}
	})

	t.Run("negative disables", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		bot := &fakeSender{}
		r := &Runner{Sources: []Source{fakeSource{name: "a", candidates: cands}}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts, MaxPostsPerRun: UnlimitedPosts}
		res, err := r.Run(context.Background())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.PostedCount() != 3 || res.CappedCount() != 0 {
			t.Errorf("posted %d capped %d, want 3/0", res.PostedCount(), res.CappedCount())
		}
	})

	t.Run("dry run is not capped", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		bot := &fakeSender{}
		var out bytes.Buffer
		r := &Runner{
			Sources: []Source{fakeSource{name: "a", candidates: cands}}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts,
			MaxPostsPerRun: 2, DryRun: true, DryOut: &out,
		}
		res, err := r.Run(context.Background())
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		// Dry runs write nothing, so capping them would hide every candidate
		// past the limit on each repeated run; all three must render.
		if bot.calls != 0 || res.CappedCount() != 0 {
			t.Errorf("dry run calls %d capped %d, want 0/0", bot.calls, res.CappedCount())
		}
		if got := strings.Count(out.String(), "END DRY-RUN"); got != 3 {
			t.Errorf("dry-run rendered %d announcements, want 3:\n%s", got, out.String())
		}
	})
}

func TestRunnerSeedBypassesPostCap(t *testing.T) {
	t.Parallel()

	// A first run with more history than the cap must still record all of
	// it: applying the cap to seeding would flood the leftovers out over the
	// following runs, the exact failure the Seeder exists to prevent.
	st := openStore(t)
	bot := &fakeSender{}
	versions := make([]string, 15)
	for i := range versions {
		versions[i] = fmt.Sprint(i + 1)
	}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", versions...)},
		pkg:        "pkg-feed",
		notice:     &Announcement{Text: "notice"},
	}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts, MaxPostsPerRun: 10}

	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SeededCount() != 15 || res.CappedCount() != 0 || res.PostedCount() != 0 || bot.calls != 1 {
		t.Fatalf("seeded %d capped %d posted %d calls %d, want 15/0/0/1", res.SeededCount(), res.CappedCount(), res.PostedCount(), bot.calls)
	}
}

func TestRunnerCapIgnoresSeenAndDeferred(t *testing.T) {
	t.Parallel()

	// Ordering contract: the cap is checked after the seen lookup (seen items
	// are reported as Seen, not Capped) and before Render (a capped item's
	// Render is never called); a deferral does not consume a slot.
	st := openStore(t)
	if err := st.Releases.RecordSeen(context.Background(), "pkg", "seen", 1, ""); err != nil {
		t.Fatalf("RecordSeen: %v", err)
	}
	rendered := map[string]bool{}
	mk := func(version string, render RenderFunc) Candidate {
		return Candidate{Source: "a", Package: "pkg", Version: version, Render: func(ctx context.Context) (*Announcement, error) {
			rendered[version] = true
			return render(ctx)
		}}
	}
	post := func(version string) RenderFunc {
		return func(context.Context) (*Announcement, error) { return announcement("pkg", version), nil }
	}
	defer_ := func(context.Context) (*Announcement, error) { return nil, Deferf("not yet") }

	bot := &fakeSender{}
	r := &Runner{
		Sources: []Source{fakeSource{name: "a", candidates: []Candidate{
			mk("deferred", defer_), mk("new1", post("new1")), mk("seen", post("seen")), mk("new2", post("new2")),
		}}},
		Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts, MaxPostsPerRun: 1,
	}
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := map[string]Outcome{"deferred": OutcomeDeferred, "new1": OutcomePosted, "seen": OutcomeSeen, "new2": OutcomeCapped}
	for _, item := range res.Items {
		if item.Outcome != want[item.Version] {
			t.Errorf("%s: outcome %s, want %s", item.Version, item.Outcome, want[item.Version])
		}
	}
	if bot.calls != 1 || rendered["new2"] || rendered["seen"] {
		t.Errorf("calls %d rendered %v; capped and seen items must not be rendered", bot.calls, rendered)
	}
}

func TestRunnerFailedSendConsumesCapSlot(t *testing.T) {
	t.Parallel()

	// A Telegram outage must stop after `limit` attempts rather than trying
	// every unseen release; the leftovers are reported as capped.
	st := openStore(t)
	bot := &fakeSender{err: errors.New("telegram down")}
	r := &Runner{
		Sources: []Source{fakeSource{name: "a", candidates: seedCandidates("a", "pkg", "1", "2", "3", "4", "5")}},
		Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts, MaxPostsPerRun: 2,
	}
	res, err := r.Run(context.Background())
	if err == nil || strings.Count(err.Error(), "telegram send") != 2 {
		t.Fatalf("expected two send errors, got %v", err)
	}
	if bot.calls != 2 || res.PostedCount() != 0 || res.CappedCount() != 3 {
		t.Errorf("calls %d posted %d capped %d, want 2/0/3", bot.calls, res.PostedCount(), res.CappedCount())
	}
}

func TestRunnerSeedEmptyNoticeIsAnError(t *testing.T) {
	t.Parallel()

	// (nil, nil) means silent seed; a non-nil Announcement with no Text is a
	// rendering bug and must fail the seed rather than seed silently.
	st := openStore(t)
	bot := &fakeSender{}
	src := &fakeSeeder{
		fakeSource: fakeSource{name: "feed", candidates: seedCandidates("feed", "pkg-feed", "1")},
		pkg:        "pkg-feed",
		notice:     &Announcement{},
	}
	r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}
	if _, err := r.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "render seed notice: empty announcement") {
		t.Fatalf("expected empty-announcement error, got %v", err)
	}
	if bot.calls != 0 {
		t.Errorf("sent %d messages", bot.calls)
	}
	if _, err := st.Releases.GetLatestSeen(context.Background(), "pkg-feed"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("failed seed wrote rows: %v", err)
	}
}

func TestRunnerSeedValidatesCandidates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cands   []Candidate
		wantErr string
	}{
		{name: "empty version", cands: []Candidate{candidate("feed", "pkg-feed", "")}, wantErr: "version is required"},
		{name: "nil render", cands: []Candidate{{Source: "feed", Package: "pkg-feed", Version: "1"}}, wantErr: "render func is required"},
		{name: "package mismatch", cands: seedCandidates("feed", "other-pkg", "1"), wantErr: "does not match seed package pkg-feed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := openStore(t)
			bot := &fakeSender{}
			src := &fakeSeeder{fakeSource: fakeSource{name: "feed", candidates: tc.cands}, pkg: "pkg-feed", notice: &Announcement{Text: "n"}}
			r := &Runner{Sources: []Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}
			_, err := r.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q, got %v", tc.wantErr, err)
			}
			if bot.calls != 0 {
				t.Errorf("sent %d messages", bot.calls)
			}
			for _, pkg := range []string{"pkg-feed", "other-pkg"} {
				if _, err := st.Releases.GetLatestSeen(context.Background(), pkg); !errors.Is(err, store.ErrNotFound) {
					t.Errorf("invalid seed wrote rows for %s: %v", pkg, err)
				}
			}
		})
	}
}

func TestOutcomeString(t *testing.T) {
	t.Parallel()
	for o, want := range map[Outcome]string{
		OutcomeError: "error", OutcomePosted: "posted", OutcomeSeen: "seen",
		OutcomeDeferred: "deferred", OutcomeSeeded: "seeded", OutcomeCapped: "capped", Outcome(42): "outcome(42)",
	} {
		if got := o.String(); got != want {
			t.Errorf("Outcome(%d).String() = %q, want %q", uint8(o), got, want)
		}
	}
	// A failed item keeps the zero value, so it is never counted as a success.
	var failed ItemResult
	if failed.Outcome != OutcomeError {
		t.Errorf("zero ItemResult outcome = %s, want error", failed.Outcome)
	}
}

func TestRunnerNonSeederSourcePostsHistory(t *testing.T) {
	t.Parallel()

	// Regression guard: plain sources keep the old behaviour and post every
	// unseen candidate on their first run.
	st := openStore(t)
	bot := &fakeSender{}
	r := &Runner{
		Sources:  []Source{fakeSource{name: "plain", candidates: seedCandidates("plain", "pkg", "1", "2")}},
		Channel:  "@ch",
		Bot:      bot,
		Releases: st.Releases,
		Posts:    st.Posts,
	}
	res, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.PostedCount() != 2 || res.SeededCount() != 0 || bot.calls != 2 {
		t.Errorf("plain source: posted %d seeded %d calls %d", res.PostedCount(), res.SeededCount(), bot.calls)
	}
}

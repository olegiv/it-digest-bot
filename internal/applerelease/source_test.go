package applerelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newTestSource(t *testing.T, platform Platform, body []byte) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP(), platform)
	src.Client.SetFeedURL(srv.URL)
	return src
}

func TestSourceCandidatesPerPlatformOldestFirst(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		platform Platform
		versions []string
		legacy   []bool
	}{
		{PlatformIOS, []string{"18.7.10", "26.6.2", "27.0.1"}, []bool{true, true, false}},
		{PlatformMacOS, []string{"26.6.2", "27.0.1"}, []bool{true, false}},
	} {
		src := newTestSource(t, tc.platform, feedWith(liveItems...))
		pkg := tc.platform.PackageKey()
		if src.Name() != pkg || src.SeedPackage() != pkg {
			t.Fatalf("Name/SeedPackage = %q/%q, want %q", src.Name(), src.SeedPackage(), pkg)
		}
		cands, err := src.Candidates(context.Background())
		if err != nil {
			t.Fatalf("%s Candidates: %v", tc.platform, err)
		}
		if len(cands) != len(tc.versions) {
			t.Fatalf("%s candidates = %d, want %d", tc.platform, len(cands), len(tc.versions))
		}
		for i, c := range cands {
			if c.Version != tc.versions[i] || c.Package != pkg || c.Source != pkg {
				t.Errorf("%s cands[%d] = %s/%s/%s, want %s/%s", tc.platform, i, c.Source, c.Package, c.Version, pkg, tc.versions[i])
			}
			ann, err := c.Render(context.Background())
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got := ann.Payload["legacy"]; got != tc.legacy[i] {
				t.Errorf("%s %s legacy = %v, want %v", tc.platform, c.Version, got, tc.legacy[i])
			}
			if ann.DryRunTitle != string(tc.platform)+" "+c.Version || ann.ReleaseURL != c.URL {
				t.Errorf("%s announcement title/url = %q/%q", tc.platform, ann.DryRunTitle, ann.ReleaseURL)
			}
		}
	}
}

func TestSourceDropsDuplicateVersions(t *testing.T) {
	t.Parallel()
	reissued := item("iOS 27.0.1 (24A448)", "10012026a", "Thu, 01 Oct 2026 10:00:00 PDT", "")
	src := newTestSource(t, PlatformIOS, feedWith(reissued, liveItems[4]))
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates = %d, want 1", len(cands))
	}
	ann, _ := cands[0].Render(context.Background())
	if ann.Payload["build"] != "24A448" {
		t.Errorf("build = %v, want the newest build 24A448", ann.Payload["build"])
	}
}

func TestSourceSeedNotice(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, PlatformIOS, feedWith(liveItems...))
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	ann, err := src.SeedNotice(context.Background(), cands)
	if err != nil {
		t.Fatalf("SeedNotice: %v", err)
	}
	if ann.Payload["newest"] != "27.0.1" || ann.Payload["count"] != 3 || ann.ReleaseURL != ReleasesPageURL {
		t.Errorf("payload = %+v", ann.Payload)
	}
	if !strings.Contains(ann.Text, "latest: `27.0.1`") {
		t.Errorf("seed text:\n%s", ann.Text)
	}

	ann, err = src.SeedNotice(context.Background(), []releasewatch.Candidate{{Version: "unknown"}})
	if err != nil {
		t.Fatalf("SeedNotice(unknown): %v", err)
	}
	if _, ok := ann.Payload["newest"]; ok {
		t.Errorf("unknown candidate must not set newest: %+v", ann.Payload)
	}
}

func TestSourceFetchError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP(), PlatformMacOS)
	src.Client.SetFeedURL(srv.URL)
	if _, err := src.Candidates(context.Background()); err == nil {
		t.Fatal("want error")
	}
}

type recordingSender struct {
	messages []string
}

func (s *recordingSender) SendMessage(_ context.Context, _, text string, _ telegram.ParseMode) (int64, error) {
	s.messages = append(s.messages, text)
	return int64(len(s.messages)), nil
}

// TestSourcesThroughRunner runs both platform sources through the real
// Runner and store: the first run records all five releases and posts one notice per platform; a
// second run with a new iOS release posts exactly that release.
func TestSourcesThroughRunner(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	body := feedWith(liveItems...)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	sources := make([]releasewatch.Source, 0, len(Platforms))
	for _, p := range Platforms {
		src := NewSource(testHTTP(), p)
		src.Client.SetFeedURL(srv.URL)
		sources = append(sources, src)
	}
	bot := &recordingSender{}
	r := &releasewatch.Runner{Sources: sources, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if res.SeededCount() != 5 || res.PostedCount() != 0 || len(bot.messages) != 2 {
		t.Fatalf("first run seeded %d posted %d messages %d, want 5/0/2", res.SeededCount(), res.PostedCount(), len(bot.messages))
	}
	if seen, _ := st.Releases.HasSeen(ctx, "macos", "26.6.2"); !seen {
		t.Error("seeded macOS release not recorded under (macos, version)")
	}

	mu.Lock()
	body = feedWith(append([]string{item("iOS 27.1 (24B90)", "10222026a", "Thu, 22 Oct 2026 10:00:00 PDT", "/go/?id=ios-27_1-rn")}, liveItems...)...)
	mu.Unlock()

	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.PostedCount() != 1 || len(bot.messages) != 3 {
		t.Fatalf("second run posted %d messages %d, want 1/3", res.PostedCount(), len(bot.messages))
	}
	if !strings.Contains(bot.messages[2], "*iOS* `27.1` released") {
		t.Errorf("second run should post iOS 27.1:\n%s", bot.messages[2])
	}

	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if res.PostedCount() != 0 || len(bot.messages) != 3 {
		t.Errorf("third run posted %d messages %d, want 0/3", res.PostedCount(), len(bot.messages))
	}
}

// TestSourceSeedsSilentlyWhenEmpty: a feed window without a stable release
// for the platform must not post a "0 releases" notice on every run.
func TestSourceSeedsSilentlyWhenEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	src := newTestSource(t, PlatformMacOS, feedWith(liveItems[1], liveItems[4])) // iOS RC + iOS stable only
	bot := &recordingSender{}
	r := &releasewatch.Runner{Sources: []releasewatch.Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}
	for range 2 {
		if _, err := r.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}
	if len(bot.messages) != 0 {
		t.Errorf("empty platform posted %d messages:\n%s", len(bot.messages), strings.Join(bot.messages, "\n---\n"))
	}
}

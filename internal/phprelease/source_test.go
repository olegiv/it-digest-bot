package phprelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newTestSource(t *testing.T, body string) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP())
	src.Client.SetActiveURL(srv.URL)
	return src
}

func TestSourceCandidates(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, activeJSON)
	if src.Name() != PackageKey || src.SeedPackage() != PackageKey {
		t.Fatalf("Name/SeedPackage = %q/%q", src.Name(), src.SeedPackage())
	}
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 5 {
		t.Fatalf("candidates = %d, want 5", len(cands))
	}
	for i, c := range cands {
		if c.Package != PackageKey || c.Source != PackageKey || c.URL == "" {
			t.Errorf("cands[%d] = %+v", i, c)
		}
		ann, err := c.Render(context.Background())
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		wantCurrent := c.Version == "8.6.0"
		if ann.Payload["current"] != wantCurrent {
			t.Errorf("%s current = %v, want %v", c.Version, ann.Payload["current"], wantCurrent)
		}
		if ann.DryRunTitle != "PHP "+c.Version || ann.ReleaseURL != c.URL {
			t.Errorf("%s title/url = %q/%q", c.Version, ann.DryRunTitle, ann.ReleaseURL)
		}
		if ann.Payload["date"] != "2026-11-19" || ann.Payload["announcement"] == nil {
			t.Errorf("%s payload = %+v", c.Version, ann.Payload)
		}
	}
	if cands[0].Version != "8.2.36" || cands[4].Version != "8.6.0" {
		t.Errorf("order = %s..%s, want oldest branch first", cands[0].Version, cands[4].Version)
	}
}

func TestSourceSeedNotice(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, activeJSON)
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	ann, err := src.SeedNotice(context.Background(), cands)
	if err != nil {
		t.Fatalf("SeedNotice: %v", err)
	}
	if ann.Payload["count"] != 5 || ann.ReleaseURL != SupportedVersionsURL {
		t.Errorf("payload = %+v", ann.Payload)
	}
	for _, want := range []string{"5 supported branches", "`8.2.36`", "`8.6.0`", `\#PHP`} {
		if !strings.Contains(ann.Text, want) {
			t.Errorf("seed text missing %q:\n%s", want, ann.Text)
		}
	}
	ann, err = src.SeedNotice(context.Background(), []releasewatch.Candidate{{Version: "unknown"}})
	if err != nil || len(ann.Payload["versions"].([]string)) != 0 {
		t.Errorf("unknown candidate: %+v, %v", ann, err)
	}
}

func TestSourceFetchError(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, `{"8":{"8.5":{"version":"garbage"}}}`)
	if _, err := src.Candidates(context.Background()); err == nil {
		t.Fatal("want error when no branch is valid")
	}
	if (&Source{}).client() == nil || (&Source{}).logger() == nil {
		t.Fatal("zero Source must fall back to defaults")
	}
}

type recordingSender struct {
	mu       sync.Mutex
	messages []string
}

func (s *recordingSender) SendMessage(_ context.Context, _, text string, _ telegram.ParseMode) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, text)
	return int64(len(s.messages)), nil
}

// TestSourceThroughRunner runs the source through the real Runner and store:
// the first run records the five branch tips and posts one notice; a
// coordinated release of two branches posts exactly those two, in version
// order; a repeat run posts nothing.
func TestSourceThroughRunner(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	body := activeJSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	ctx := context.Background()
	st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "php.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	src := NewSource(testHTTP())
	src.Client.SetActiveURL(srv.URL)
	bot := &recordingSender{}
	r := &releasewatch.Runner{Sources: []releasewatch.Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if res.SeededCount() != 5 || res.PostedCount() != 0 || len(bot.messages) != 1 {
		t.Fatalf("first run seeded %d posted %d messages %d, want 5/0/1", res.SeededCount(), res.PostedCount(), len(bot.messages))
	}

	mu.Lock()
	body = strings.NewReplacer(`"8.5.13"`, `"8.5.14"`, `"8.6.0"`, `"8.6.1"`).Replace(activeJSON)
	mu.Unlock()

	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.PostedCount() != 2 || len(bot.messages) != 3 {
		t.Fatalf("second run posted %d messages %d, want 2/3", res.PostedCount(), len(bot.messages))
	}
	if !strings.Contains(bot.messages[1], "`8.5.14`") || !strings.Contains(bot.messages[2], "`8.6.1`") {
		t.Errorf("unexpected posts:\n%s\n---\n%s", bot.messages[1], bot.messages[2])
	}
	if !strings.Contains(bot.messages[1], "Security release") || strings.Contains(bot.messages[2], "Security release") {
		t.Error("security flag must follow the release tag")
	}

	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if res.PostedCount() != 0 || len(bot.messages) != 3 {
		t.Errorf("third run posted %d messages %d, want 0/3", res.PostedCount(), len(bot.messages))
	}
}

func TestFormatRelease(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	sec := &Release{Branch: "8.4", Version: "8.4.28", Date: date, Security: true, Announced: true}
	got := FormatRelease(sec, false)
	for _, want := range []string{
		"🐘 *PHP* `8.4.28` released\n",
		"Update for the older 8\\.4 branch\n",
		"🔒 *Security release*",
		"Released: 2026\\-11\\-19\n",
		"[Announcement](https://www.php.net/releases/8_4_28.php)",
		"[ChangeLog](https://www.php.net/ChangeLog-8.php#8.4.28)",
		"[Downloads](https://www.php.net/downloads.php)",
		`\#PHP \#Security`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("security post missing %q:\n%s", want, got)
		}
	}

	branch := &Release{Branch: "8.6", Version: "8.6.0"}
	got = FormatRelease(branch, true)
	if !strings.Contains(got, "🎉 First release of the new PHP 8\\.6 branch") {
		t.Errorf("new-branch post:\n%s", got)
	}
	if strings.Contains(got, "Announcement") || strings.Contains(got, "Released:") || strings.Contains(got, "Security") {
		t.Errorf("post must omit absent announcement/date/security:\n%s", got)
	}

	cur := FormatRelease(&Release{Branch: "8.6", Version: "8.6.1"}, true)
	if !strings.Contains(cur, "Current 8\\.6 branch") || !strings.HasSuffix(cur, `\#PHP`) {
		t.Errorf("current-branch post:\n%s", cur)
	}
}

func TestFormatSeedNoticeSingular(t *testing.T) {
	t.Parallel()
	got := FormatSeedNotice([]*Release{{Version: "8.5.13"}})
	if !strings.Contains(got, "1 supported branch as") {
		t.Errorf("singular seed notice:\n%s", got)
	}
	if strings.Contains(FormatSeedNotice(nil), ": ") {
		t.Error("empty seed notice must not list versions")
	}
}

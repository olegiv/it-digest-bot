package drupalsec

import (
	"context"
	"fmt"
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

func newTestSource(t *testing.T, body string) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP())
	src.Client.SetFeedURL(srv.URL)
	return src
}

func TestSourceCandidatesOldestFirst(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, liveFeedXML)
	if src.Name() != "drupal-security" || src.SeedPackage() != PackageKey {
		t.Fatalf("Name/SeedPackage = %q/%q", src.Name(), src.SeedPackage())
	}

	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	want := []string{"SA-CORE-2026-013", "PSA-2026-09-21", "SA-CONTRIB-2026-184", "SA-CONTRIB-2026-191"}
	if len(cands) != len(want) {
		t.Fatalf("candidates = %d, want %d", len(cands), len(want))
	}
	for i, c := range cands {
		if c.Version != want[i] {
			t.Errorf("cands[%d].Version = %q, want %q (oldest first)", i, c.Version, want[i])
		}
		if c.Package != PackageKey || c.Source != "drupal-security" {
			t.Errorf("cands[%d] package/source = %q/%q", i, c.Package, c.Source)
		}
		if !strings.HasPrefix(c.URL, "https://www.drupal.org/") {
			t.Errorf("cands[%d].URL = %q", i, c.URL)
		}
		if c.Render == nil {
			t.Fatalf("cands[%d] has no Render", i)
		}
	}

	ann, err := cands[2].Render(context.Background())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(ann.Text, "SA\\-CONTRIB\\-2026\\-184") || ann.ReleaseURL != "https://www.drupal.org/sa-contrib-2026-184" {
		t.Errorf("announcement = %+v", ann)
	}
	if ann.DryRunTitle != "Drupal SA-CONTRIB-2026-184" {
		t.Errorf("DryRunTitle = %q", ann.DryRunTitle)
	}
	if ann.Payload["kind"] != "contrib" || ann.Payload["project"] != "tawk_to" || ann.Payload["risk"] != "Critical" {
		t.Errorf("payload = %v", ann.Payload)
	}
}

func TestSourceSeedNotice(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, liveFeedXML)
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	ann, err := src.SeedNotice(context.Background(), cands)
	if err != nil {
		t.Fatalf("SeedNotice: %v", err)
	}
	if !strings.Contains(ann.Text, "Recorded 4 advisories") || !strings.Contains(ann.Text, "newest: SA\\-CONTRIB\\-2026\\-191") {
		t.Errorf("seed notice text:\n%s", ann.Text)
	}
	if ann.ReleaseURL != SecurityPageURL || ann.Payload["count"] != 4 || ann.Payload["newest"] != "SA-CONTRIB-2026-191" {
		t.Errorf("seed announcement = %+v", ann)
	}

	// Candidates the source does not know (defensive) are ignored.
	ann, err = src.SeedNotice(context.Background(), []releasewatch.Candidate{{Version: "unknown"}})
	if err != nil || !strings.Contains(ann.Text, "Recorded 1 advisory ") || strings.Contains(ann.Text, "newest") {
		t.Errorf("seed notice for unknown candidate: %v / %s", err, ann.Text)
	}
}

func TestSourceCandidatesFetchError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	src := NewSource(testHTTP())
	src.Client.SetFeedURL(srv.URL)
	if _, err := src.Candidates(context.Background()); err == nil {
		t.Error("expected fetch error")
	}
}

func TestSourceCandidatesSkipsBrokenItems(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, string(feedWith(edgeItems)))
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, want 2 (two items skipped with warnings)", len(cands))
	}
	// Zero published time sorts first; the guid-derived version is stable.
	if cands[0].Version != "SA-CONTRIB-2026-802" || cands[1].Version != "nid-8000003" {
		t.Errorf("versions = %q, %q", cands[0].Version, cands[1].Version)
	}
}

// recordingSender is the minimal releasewatch.Sender for end-to-end tests.
type recordingSender struct {
	messages []string
}

func (s *recordingSender) SendMessage(_ context.Context, _ string, text string, _ telegram.ParseMode) (int64, error) {
	s.messages = append(s.messages, text)
	return int64(len(s.messages)), nil
}

// TestSourceThroughRunner runs the real Source through the real Runner with
// a real store: first run seeds the feed and posts one notice; a second run
// with one new item posts exactly that item. This is the only test that
// exercises the Candidates → SeedNotice → releases_seen key agreement end to
// end.
func TestSourceThroughRunner(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	body := string(feedWith(handcraftedItem))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, body)
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

	src := NewSource(testHTTP())
	src.Client.SetFeedURL(srv.URL)
	bot := &recordingSender{}
	r := &releasewatch.Runner{Sources: []releasewatch.Source{src}, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts}

	res, err := r.Run(ctx)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if res.SeededCount() != 1 || res.PostedCount() != 0 || len(bot.messages) != 1 {
		t.Fatalf("first run seeded %d posted %d messages %d, want 1/0/1", res.SeededCount(), res.PostedCount(), len(bot.messages))
	}
	if !strings.Contains(bot.messages[0], "are now tracked here") || !strings.Contains(bot.messages[0], "newest: SA\\-CONTRIB\\-2026\\-901") {
		t.Errorf("seed notice wrong:\n%s", bot.messages[0])
	}
	if seen, _ := st.Releases.HasSeen(ctx, PackageKey, "SA-CONTRIB-2026-901"); !seen {
		t.Error("seeded advisory not recorded under (PackageKey, ID)")
	}

	mu.Lock()
	body = string(feedWith(handcraftedItem, unstructuredItem))
	mu.Unlock()

	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.PostedCount() != 1 || res.SeededCount() != 0 || len(bot.messages) != 2 {
		t.Fatalf("second run posted %d seeded %d messages %d, want 1/0/2", res.PostedCount(), res.SeededCount(), len(bot.messages))
	}
	if !strings.Contains(bot.messages[1], "SA\\-CONTRIB\\-2026\\-902") || !strings.Contains(bot.messages[1], "[Advisory](https://www.drupal.org/sa-contrib-2026-902)") {
		t.Errorf("second run should post the new advisory:\n%s", bot.messages[1])
	}
	if seen, _ := st.Releases.HasSeen(ctx, PackageKey, "SA-CONTRIB-2026-902"); !seen {
		t.Error("posted advisory not recorded")
	}

	// Third run: nothing new, nothing sent.
	res, err = r.Run(ctx)
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if res.PostedCount() != 0 || len(bot.messages) != 2 {
		t.Errorf("third run posted %d messages %d, want 0/2", res.PostedCount(), len(bot.messages))
	}
}

const samePubDateItems = `<item>
<title>Later Alpha - Critical - XSS - SA-CONTRIB-2026-950</title>
<link>https://www.drupal.org/sa-contrib-2026-950</link>
<description>&lt;p&gt;a&lt;/p&gt;</description>
<pubDate>Wed, 23 Sep 2026 17:00:00 +0000</pubDate>
<guid isPermaLink="false">8000950 at https://www.drupal.org</guid>
</item>
<item>
<title>Earlier Alpha - Critical - XSS - SA-CONTRIB-2026-949</title>
<link>https://www.drupal.org/sa-contrib-2026-949</link>
<description>&lt;p&gt;b&lt;/p&gt;</description>
<pubDate>Wed, 23 Sep 2026 17:00:00 +0000</pubDate>
<guid isPermaLink="false">8000949 at https://www.drupal.org</guid>
</item>`

func TestSourceCandidatesTieBreakOnVersion(t *testing.T) {
	t.Parallel()
	src := newTestSource(t, string(feedWith(samePubDateItems)))
	cands, err := src.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(cands) != 2 || cands[0].Version != "SA-CONTRIB-2026-949" || cands[1].Version != "SA-CONTRIB-2026-950" {
		t.Errorf("equal pubDate must sort by version: %+v", cands)
	}
}

func TestSourceLazyClient(t *testing.T) {
	t.Parallel()
	src := &Source{}
	if src.client().FeedURL() != DefaultFeedURL {
		t.Errorf("lazy client feed URL = %q", src.client().FeedURL())
	}
}

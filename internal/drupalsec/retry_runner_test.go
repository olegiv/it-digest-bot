package drupalsec

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
)

// This source runs after Drupal to prove a persistent 404 cannot prevent
// another source from posting (or rendering, when the runner is dry).
type healthyRetrySource struct{}

func (healthyRetrySource) Name() string { return "healthy" }

func (healthyRetrySource) Candidates(context.Context) ([]releasewatch.Candidate, error) {
	return []releasewatch.Candidate{{
		Package: "healthy",
		Version: "1.0.0",
		Render: func(context.Context) (*releasewatch.Announcement, error) {
			return &releasewatch.Announcement{Text: "Healthy release", DryRunTitle: "Healthy release"}, nil
		},
	}}, nil
}

func TestSourceNotFoundRetryThroughRunner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		initialized bool
		fail        bool
	}{
		{name: "recover and seed"},
		{name: "recover and post", initialized: true},
		{name: "exhaustion before seeding", fail: true},
		{name: "exhaustion with history", initialized: true, fail: true},
	} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", tc.name, dryRun), func(t *testing.T) {
				t.Parallel()
				var requests atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					n := requests.Add(1)
					if tc.fail || n <= 2 {
						http.Error(w, "not found", http.StatusNotFound)
						return
					}
					fmt.Fprint(w, string(feedWith(handcraftedItem)))
				}))
				t.Cleanup(srv.Close)
				ctx := context.Background()
				st, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "retry.db"))
				if err != nil {
					t.Fatalf("open store: %v", err)
				}
				t.Cleanup(func() { _ = st.Close() })
				if err := st.Migrate(ctx); err != nil {
					t.Fatalf("migrate: %v", err)
				}
				wantRows := 0
				if tc.initialized {
					if err := st.Releases.RecordSeen(ctx, PackageKey, "existing", 42, "https://www.drupal.org/security"); err != nil {
						t.Fatalf("record baseline: %v", err)
					}
					wantRows++
				}
				src := NewSource(testHTTP())
				src.Client.SetFeedURL(srv.URL)
				src.Client.sleep = nopSleep
				sources := []releasewatch.Source{src}
				if tc.fail {
					sources = append(sources, healthyRetrySource{})
				}
				var out bytes.Buffer
				bot := &recordingSender{}
				runner := &releasewatch.Runner{
					Sources: sources, Channel: "@ch", Bot: bot, Releases: st.Releases, Posts: st.Posts,
					DryRun: dryRun, DryOut: &out,
				}
				res, err := runner.Run(ctx)
				if tc.fail {
					if err == nil || !strings.Contains(err.Error(), "source drupal-security candidates: drupal security feed: http 404 after 3 attempts") {
						t.Fatalf("Run error = %v, want exhausted Drupal source error", err)
					}
				} else if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if got := requests.Load(); got != 3 {
					t.Errorf("Drupal requests = %d, want 3", got)
				}
				wantMessages, wantPosts, wantSeeds := 0, 0, 0
				if !dryRun {
					wantMessages = 1
					wantRows++
					if tc.fail || tc.initialized {
						wantPosts = 1
					} else {
						wantSeeds = 1
					}
				}
				if len(bot.messages) != wantMessages || res.PostedCount() != wantPosts {
					t.Errorf("messages/posted = %d/%d, want %d/%d", len(bot.messages), res.PostedCount(), wantMessages, wantPosts)
				}
				for _, check := range []struct {
					pkg, version string
					seen         bool
				}{
					{PackageKey, "existing", tc.initialized},
					{PackageKey, "SA-CONTRIB-2026-901", !dryRun && !tc.fail},
					{"healthy", "1.0.0", !dryRun && tc.fail},
				} {
					seen, err := st.Releases.HasSeen(ctx, check.pkg, check.version)
					if err != nil || seen != check.seen {
						t.Errorf("HasSeen(%s, %s) = %t/%v, want %t", check.pkg, check.version, seen, err, check.seen)
					}
				}
				var rows int
				if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM releases_seen").Scan(&rows); err != nil || rows != wantRows {
					t.Fatalf("release rows = %d/%v, want %d", rows, err, wantRows)
				}
				for kind, want := range map[store.Kind]int{store.KindRelease: wantPosts, store.KindSeed: wantSeeds} {
					if got, err := st.Posts.Count(ctx, kind); err != nil || got != want {
						t.Errorf("%s audit rows = %d/%v, want %d", kind, got, err, want)
					}
				}
				if dryRun && out.Len() == 0 {
					t.Error("dry run did not render the recovered feed or healthy source")
				}
				if !dryRun && !tc.fail {
					// A retry must not cause duplicate posts or seed notices later.
					res, err = runner.Run(ctx)
					if err != nil || res.PostedCount() != 0 || res.SeededCount() != 0 || len(bot.messages) != 1 {
						t.Errorf("repeat run posted/seeded/messages/error = %d/%d/%d/%v", res.PostedCount(), res.SeededCount(), len(bot.messages), err)
					}
				}
			})
		}
	}
}

package drupalsec

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olegiv/it-digest-bot/internal/releasewatch"
)

func newTestSource(t *testing.T, body string) *Source {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	src := NewSource(testHTTP())
	src.Client.WithFeedURL(srv.URL)
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
	src.Client.WithFeedURL(srv.URL)
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

func TestSourceLazyClient(t *testing.T) {
	t.Parallel()
	src := &Source{}
	if src.client().FeedURL() != DefaultFeedURL {
		t.Errorf("lazy client feed URL = %q", src.client().FeedURL())
	}
}

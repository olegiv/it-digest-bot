package gorelease

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func testHTTP() *httpx.Client {
	return httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0))
}

func TestFetchStableFiltersStableReleases(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		fmt.Fprint(w, `[
			{"version":"go1.26.2","stable":true,"files":[]},
			{"version":"go1.27rc1","stable":false,"files":[]},
			{"version":"go1.25.9","stable":true,"files":[]}
		]`)
	}))
	defer srv.Close()

	releases, err := NewClient(testHTTP()).WithURLs(srv.URL, "unused").FetchStable(context.Background())
	if err != nil {
		t.Fatalf("FetchStable: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("stable releases = %d, want 2", len(releases))
	}
	if releases[0].Version != "go1.26.2" || releases[1].Version != "go1.25.9" {
		t.Fatalf("versions = %#v", releases)
	}
}

func TestFetchStableRejectsEmptyStableSet(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"version":"go1.27rc1","stable":false}]`)
	}))
	defer srv.Close()

	_, err := NewClient(testHTTP()).WithURLs(srv.URL, "unused").FetchStable(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no stable releases") {
		t.Fatalf("expected no stable releases error, got %v", err)
	}
}

func TestFetchStableRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{not-json`)
	}))
	defer srv.Close()

	_, err := NewClient(testHTTP()).WithURLs(srv.URL, "unused").FetchStable(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode go downloads response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestExtractReleaseSummaryMinor(t *testing.T) {
	t.Parallel()

	page := []byte(`<html><body>
		<p id="go1.26.2">
			go1.26.2 (released 2026-04-07) includes
			security fixes to the <code>go</code> command.
			See the <a href="https://github.com/golang/go/issues">Go 1.26.2 milestone</a>.
		</p>
	</body></html>`)

	summary, err := ExtractReleaseSummary(page, "go1.26.2")
	if err != nil {
		t.Fatalf("ExtractReleaseSummary: %v", err)
	}
	for _, want := range []string{
		"go1.26.2 (released 2026-04-07)",
		"security fixes to the go command",
		"Go 1.26.2 milestone",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q: %s", want, summary)
		}
	}
}

func TestExtractReleaseSummaryMajorIncludesFollowingParagraph(t *testing.T) {
	t.Parallel()

	page := []byte(`<html><body>
		<h2 id="go1.26.0">go1.26.0 (released 2026-02-10)</h2>
		<p>Go 1.26.0 is a major release of Go. Read the release notes.</p>
	</body></html>`)

	summary, err := ExtractReleaseSummary(page, "go1.26.0")
	if err != nil {
		t.Fatalf("ExtractReleaseSummary: %v", err)
	}
	if !strings.Contains(summary, "Go 1.26.0 is a major release") {
		t.Fatalf("summary did not include paragraph: %s", summary)
	}
}

func TestSourceUsesFallbackWhenHistoryUnavailable(t *testing.T) {
	t.Parallel()

	downloads := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"version":"go1.99.1","stable":true,"files":[]}]`)
	}))
	defer downloads.Close()

	history := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer history.Close()

	source := &Source{
		Client: NewClient(testHTTP()).WithURLs(downloads.URL, history.URL),
	}
	candidates, err := source.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	ann, err := candidates[0].Render(context.Background())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(ann.Text, "Official stable") {
		t.Fatalf("fallback summary missing from announcement:\n%s", ann.Text)
	}
	if ann.ReleaseURL != "https://go.dev/doc/devel/release#go1.99.1" {
		t.Fatalf("release URL = %q", ann.ReleaseURL)
	}
}

func TestSourceRendersWithHistorySummary(t *testing.T) {
	t.Parallel()

	downloads := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[
			{"version":"go1.26.2","stable":true,"files":[]},
			{"version":"go1.27rc1","stable":false,"files":[]}
		]`)
	}))
	defer downloads.Close()

	history := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body>
			<p id="go1.26.2">go1.26.2 includes security fixes to the go command.</p>
		</body></html>`)
	}))
	defer history.Close()

	source := &Source{
		Client: NewClient(testHTTP()).WithURLs(downloads.URL, history.URL),
	}
	candidates, err := source.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].Package != PackageKey || candidates[0].Version != "go1.26.2" {
		t.Fatalf("candidate = %+v", candidates[0])
	}
	ann, err := candidates[0].Render(context.Background())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(ann.Text, "security fixes") {
		t.Fatalf("history summary missing from announcement:\n%s", ann.Text)
	}
	if strings.Contains(ann.Text, "Official stable") {
		t.Fatalf("fallback summary leaked into history-success path:\n%s", ann.Text)
	}
	if got, want := ann.Payload["download_url"], "https://go.dev/dl/#go1.26.2"; got != want {
		t.Fatalf("download_url = %v, want %v", got, want)
	}
}

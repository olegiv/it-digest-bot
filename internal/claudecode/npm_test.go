package claudecode

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

func TestNPMFetchLatest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@anthropic-ai%2Fclaude-code" &&
			r.URL.Path != "/@anthropic-ai/claude-code" {
			t.Errorf("path = %q", r.URL.Path)
		}
		fmt.Fprint(w, `{
            "dist-tags": {"latest": "2.1.114"},
            "time": {"2.1.114": "2025-04-18T10:00:00.000Z"}
        }`)
	}))
	defer srv.Close()

	c := NewNPMClient(testHTTP()).WithBaseURL(srv.URL)
	info, err := c.FetchLatest(context.Background(), "@anthropic-ai/claude-code")
	if err != nil {
		t.Fatalf("FetchLatest: %v", err)
	}
	if info.LatestVersion != "2.1.114" {
		t.Errorf("version = %q", info.LatestVersion)
	}
	if info.PublishedAt.IsZero() {
		t.Errorf("publishedAt not set")
	}
}

func TestNPMFetchLatestMissingTag(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"dist-tags": {}, "time": {}}`)
	}))
	defer srv.Close()

	c := NewNPMClient(testHTTP()).WithBaseURL(srv.URL)
	_, err := c.FetchLatest(context.Background(), "nothing")
	if err == nil || !strings.Contains(err.Error(), "dist-tags.latest") {
		t.Errorf("expected dist-tags.latest error, got %v", err)
	}
}

func TestNPMFetchLatest500(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewNPMClient(testHTTP()).WithBaseURL(srv.URL)
	_, err := c.FetchLatest(context.Background(), "pkg")
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

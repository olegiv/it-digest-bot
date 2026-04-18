package news

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func testHTTP() *httpx.Client {
	return httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0))
}

func rssServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, body)
	}))
}

const rssTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>%s</title>
    <link>https://example.com</link>
    <description>test</description>
    <item>
      <title>Fresh news</title>
      <link>https://example.com/fresh</link>
      <description>Fresh body</description>
      <pubDate>%s</pubDate>
    </item>
    <item>
      <title>Old news</title>
      <link>https://example.com/old</link>
      <description>Old body</description>
      <pubDate>%s</pubDate>
    </item>
  </channel>
</rss>`

func TestFeedFetcherBasic(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	fresh := now.Add(-2 * time.Hour).Format(time.RFC1123Z)
	old := now.Add(-48 * time.Hour).Format(time.RFC1123Z)

	srv := rssServer(t, fmt.Sprintf(rssTmpl, "TestFeed", fresh, old))
	defer srv.Close()

	ff := NewFeedFetcher(
		[]Feed{{Name: "TestFeed", URL: srv.URL}},
		testHTTP(),
		WithLookback(24*time.Hour),
	)
	items, err := ff.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (only the fresh one)", len(items))
	}
	if items[0].Title != "Fresh news" {
		t.Errorf("title = %q", items[0].Title)
	}
	if items[0].Source != "TestFeed" {
		t.Errorf("source = %q", items[0].Source)
	}
	if items[0].URL != "https://example.com/fresh" {
		t.Errorf("url = %q", items[0].URL)
	}
}

func TestFeedFetcherParallel(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	fresh := now.Add(-2 * time.Hour).Format(time.RFC1123Z)
	old := now.Add(-48 * time.Hour).Format(time.RFC1123Z)

	feeds := []Feed{}
	var servers []*httptest.Server
	for i := range 6 {
		name := fmt.Sprintf("feed-%d", i)
		srv := rssServer(t, fmt.Sprintf(rssTmpl, name, fresh, old))
		feeds = append(feeds, Feed{Name: name, URL: srv.URL})
		servers = append(servers, srv)
	}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()

	ff := NewFeedFetcher(feeds, testHTTP(), WithConcurrency(2))
	items, err := ff.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(items) != 6 {
		t.Errorf("got %d items, want 6 (1 fresh per feed × 6 feeds)", len(items))
	}

	seenSources := map[string]bool{}
	for _, it := range items {
		seenSources[it.Source] = true
	}
	if len(seenSources) != 6 {
		t.Errorf("expected items from 6 distinct sources, got %d", len(seenSources))
	}
}

func TestFeedFetcherPartialFailure(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	fresh := now.Add(-2 * time.Hour).Format(time.RFC1123Z)
	old := now.Add(-48 * time.Hour).Format(time.RFC1123Z)
	good := rssServer(t, fmt.Sprintf(rssTmpl, "Good", fresh, old))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer bad.Close()

	ff := NewFeedFetcher(
		[]Feed{{Name: "Good", URL: good.URL}, {Name: "Bad", URL: bad.URL}},
		testHTTP(),
	)
	items, err := ff.FetchAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Bad") {
		t.Errorf("expected error mentioning Bad, got %v", err)
	}
	if len(items) != 1 || items[0].Source != "Good" {
		t.Errorf("good feed's items not returned: %+v", items)
	}
}

func TestSanitizeURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw, want string
	}{
		{"https://example.com/feed.xml", "https://example.com/feed.xml"},
		{"https://example.com/feed.xml?api_key=SECRET", "https://example.com/feed.xml"},
		{"https://example.com/feed.xml#section", "https://example.com/feed.xml"},
		{"https://user:pass@example.com/feed.xml", "https://example.com/feed.xml"},
		{"https://user:pass@example.com/feed.xml?token=abc#frag", "https://example.com/feed.xml"},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if got := SanitizeURL(u); got != tc.want {
			t.Errorf("SanitizeURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestRedactURLUnparseable(t *testing.T) {
	t.Parallel()
	if got := redactURL("://not-a-url"); got != "<unparseable>" {
		t.Errorf("redactURL(invalid) = %q, want <unparseable>", got)
	}
}

func TestFeedFetcherErrorRedactsFeedURL(t *testing.T) {
	t.Parallel()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer bad.Close()

	secret := "SECRET-FEED-API-KEY"
	urlWithSecret := bad.URL + "/feed.xml?key=" + secret
	// Inject a BARE httpx.Client — no explicit sanitizer. NewFeedFetcher must
	// install SanitizeURL defensively so inner retry-exhausted errors redact.
	bareClient := httpx.New(
		httpx.WithSleep(nopSleep),
		httpx.WithMaxRetries(0),
	)
	ff := NewFeedFetcher(
		[]Feed{{Name: "Bad", URL: urlWithSecret}},
		bareClient,
	)
	_, err := ff.FetchAll(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked query-string secret: %v", err)
	}
}

func TestFeedBodyTooLarge(t *testing.T) {
	t.Parallel()
	const maxFeedBytes = 10 << 20
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		buf := make([]byte, maxFeedBytes+1024)
		for i := range buf {
			buf[i] = 'A'
		}
		_, _ = w.Write(buf)
	}))
	defer srv.Close()

	ff := NewFeedFetcher([]Feed{{Name: "Huge", URL: srv.URL}}, testHTTP())
	_, err := ff.FetchAll(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected size-limit error, got %v", err)
	}
}

func TestFeedFetcherIgnoresItemsWithoutDate(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>X</title><link>x</link><description>x</description>
<item><title>No date</title><link>https://example.com/no-date</link><description>d</description></item>
</channel></rss>`
	srv := rssServer(t, body)
	defer srv.Close()

	ff := NewFeedFetcher([]Feed{{Name: "X", URL: srv.URL}}, testHTTP())
	items, err := ff.FetchAll(context.Background())
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected items without pubDate to be filtered, got %d", len(items))
	}
}

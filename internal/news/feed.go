package news

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/mmcdole/gofeed"
	"golang.org/x/sync/errgroup"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/strs"
)

// Feed is a single configured feed source.
type Feed struct {
	Name string
	URL  string
}

// FeedFetcher fetches RSS/Atom feeds concurrently, parses them with gofeed,
// and returns items published within a lookback window. It performs no
// deduplication — that's the caller's job (via store.Articles).
type FeedFetcher struct {
	feeds       []Feed
	http        *httpx.Client
	lookback    time.Duration
	concurrency int
}

// FeedFetcherOption configures a FeedFetcher.
type FeedFetcherOption func(*FeedFetcher)

// WithLookback overrides the default 24h lookback window.
func WithLookback(d time.Duration) FeedFetcherOption {
	return func(f *FeedFetcher) { f.lookback = d }
}

// WithConcurrency overrides the default bounded concurrency (4).
func WithConcurrency(n int) FeedFetcherOption {
	return func(f *FeedFetcher) { f.concurrency = n }
}

// DefaultLookback is the fallback fetch window. 48h instead of 24h because
// many feeds (e.g. anthropic.com/news) stamp items at 00:00 UTC on the
// publication date, which makes a 24h window miss same-day items when the
// run itself is later than 00:00 UTC. articles_seen prevents duplicates
// across daily runs, so a wider window is safe.
const DefaultLookback = 48 * time.Hour

// NewFeedFetcher returns a fetcher for the given feeds. Feed URLs may embed
// API keys in the query string, so NewFeedFetcher unconditionally installs
// SanitizeURL on its httpx.Client — even if the caller provided a bare one.
// This mutates the injected client; callers who share an httpx.Client across
// unrelated services should give the fetcher its own.
func NewFeedFetcher(feeds []Feed, h *httpx.Client, opts ...FeedFetcherOption) *FeedFetcher {
	h.SetURLSanitizer(SanitizeURL)
	f := &FeedFetcher{
		feeds:       feeds,
		http:        h,
		lookback:    DefaultLookback,
		concurrency: 4,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

// FetchAll fetches every configured feed in parallel (bounded by the
// concurrency limit) and returns all items whose published time falls
// within the lookback window. Per-feed errors are logged into the returned
// error but do not prevent other feeds from being fetched.
func (f *FeedFetcher) FetchAll(ctx context.Context) ([]Item, error) {
	cutoff := time.Now().Add(-f.lookback)
	type result struct {
		items []Item
		err   error
	}
	results := make([]result, len(f.feeds))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(f.concurrency)
	for i, feed := range f.feeds {
		g.Go(func() error {
			items, err := f.fetchOne(gctx, feed, cutoff)
			results[i] = result{items: items, err: err}
			return nil
		})
	}
	_ = g.Wait()

	var items []Item
	var firstErr error
	for _, r := range results {
		items = append(items, r.items...)
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
	}
	return items, firstErr
}

func (f *FeedFetcher) fetchOne(ctx context.Context, feed Feed, cutoff time.Time) ([]Item, error) {
	body, err := f.fetchBody(ctx, feed.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s (%s): %w", feed.Name, redactURL(feed.URL), err)
	}
	parsed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", feed.Name, err)
	}
	out := make([]Item, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		published := itemPublishedTime(it)
		if published.IsZero() || published.Before(cutoff) {
			continue
		}
		url := strs.FirstNonEmpty(it.Link, it.GUID)
		if url == "" {
			continue
		}
		out = append(out, Item{
			Source:    feed.Name,
			Title:     it.Title,
			URL:       url,
			Published: published,
			Summary:   strs.FirstNonEmpty(it.Description, it.Content),
		})
	}
	return out, nil
}

func (f *FeedFetcher) fetchBody(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml")
	resp, err := f.http.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	const maxFeedBytes = 10 << 20
	buf := &bytes.Buffer{}
	n, err := buf.ReadFrom(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if n > maxFeedBytes {
		return nil, fmt.Errorf("feed body exceeds %d bytes", maxFeedBytes)
	}
	return buf.Bytes(), nil
}

// SanitizeURL strips userinfo, query string, and fragment from a feed URL.
// Feed URLs can embed API keys in query parameters, so the query must not
// leak into error messages or logs. Wire into an httpx.Client via
// httpx.WithURLSanitizer to cover inner errors from the retry path.
func SanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	cp := *u
	cp.User = nil
	cp.RawQuery = ""
	cp.Fragment = ""
	cp.RawFragment = ""
	return cp.Redacted()
}

// redactURL parses raw and delegates to SanitizeURL. Used for the outer
// "fetch <name> (<url>)" error wrapper where we only have the raw string.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	return SanitizeURL(u)
}

func itemPublishedTime(it *gofeed.Item) time.Time {
	if it.PublishedParsed != nil {
		return *it.PublishedParsed
	}
	if it.UpdatedParsed != nil {
		return *it.UpdatedParsed
	}
	return time.Time{}
}

// Package news aggregates RSS/Atom feeds for the daily digest.
//
// The production Fetcher lives in feed.go (FeedFetcher). Dedup against
// articles_seen is the caller's responsibility (see internal/digest).
package news

import (
	"context"
	"time"
)

// Item is a single feed entry after parsing.
type Item struct {
	Source    string
	Title     string
	URL       string
	Published time.Time
	Summary   string
}

// Fetcher fetches and parses the configured feeds.
type Fetcher interface {
	FetchAll(ctx context.Context) ([]Item, error)
}

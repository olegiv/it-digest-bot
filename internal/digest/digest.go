// Package digest builds the daily AI news post: fetch feeds, dedupe,
// summarize via Claude, render MarkdownV2 grouped by source, split into
// messages under Telegram's 4096-byte cap, and post each chunk.
package digest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// Builder orchestrates the daily digest end to end.
type Builder struct {
	Fetcher    news.Fetcher
	Summarizer llm.Summarizer
	Bot        *telegram.Bot
	Channel    string
	Model      string
	MaxTokens  int
	Articles   *store.Articles
	Posts      *store.Posts
	Logger     *slog.Logger

	// DryRun, when true, prints rendered messages to DryOut instead of
	// sending them to Telegram, and skips all DB writes (articles_seen,
	// posts_log). Useful for pre-flight against the real feeds + Claude.
	DryRun bool
	DryOut io.Writer // defaults to os.Stdout

	// Now is injected for tests; defaults to time.Now.
	Now func() time.Time
}

// Result summarises a single digest run.
type Result struct {
	Fetched   int
	NewItems  int
	Summaries int
	Messages  int
}

// Run executes one build-and-post pass.
func (b *Builder) Run(ctx context.Context) (*Result, error) {
	log := b.logger()
	now := b.now()

	items, err := b.Fetcher.FetchAll(ctx)
	if err != nil {
		log.Warn("feed fetch had errors", "err", err, "items_returned", len(items))
	}
	log.Info("fetched items", "count", len(items))
	res := &Result{Fetched: len(items)}

	fresh, err := b.filterSeen(ctx, items)
	if err != nil {
		return res, fmt.Errorf("dedup articles: %w", err)
	}
	res.NewItems = len(fresh)
	log.Info("after dedup against articles_seen", "fresh", len(fresh))
	if len(fresh) == 0 {
		log.Info("no new items to summarize; exiting cleanly")
		return res, nil
	}

	summaries, err := b.Summarizer.Summarize(ctx, llm.SummarizeRequest{
		Model:     b.Model,
		MaxTokens: b.MaxTokens,
		Articles:  itemsToLLMArticles(fresh),
	})
	if err != nil {
		return res, fmt.Errorf("summarize: %w", err)
	}
	res.Summaries = len(summaries)
	log.Info("summaries returned", "count", len(summaries))
	if len(summaries) == 0 {
		log.Info("no summaries returned; exiting")
		return res, nil
	}

	messages, err := RenderAndSplit(now, fresh, summaries, telegram.MaxMessageBytes, log)
	if err != nil {
		return res, fmt.Errorf("render: %w", err)
	}
	res.Messages = len(messages)

	if b.DryRun {
		b.printDryRun(messages, log)
		return res, nil
	}

	for i, m := range messages {
		mid, err := b.Bot.SendMessage(ctx, b.Channel, m.Text, telegram.ParseModeMarkdownV2)
		if err != nil {
			return res, fmt.Errorf("send message %d/%d: %w", i+1, len(messages), err)
		}
		log.Info("posted digest chunk",
			"index", i+1, "of", len(messages), "message_id", mid)
		b.recordArticles(ctx, m.Articles, log)
		b.recordPostsLog(ctx, now, m, mid, log)
	}
	return res, nil
}

func (b *Builder) printDryRun(messages []Message, log *slog.Logger) {
	out := b.DryOut
	if out == nil {
		out = os.Stdout
	}
	log.Info("dry-run: rendering messages to stdout; no Telegram send, no DB writes",
		"count", len(messages))
	for i, m := range messages {
		_, _ = fmt.Fprintf(out, "\n---- MESSAGE %d/%d — %d bytes, %d articles ----\n%s\n",
			i+1, len(messages), len(m.Text), len(m.Articles), m.Text)
	}
	_, _ = fmt.Fprintln(out, "\n---- END DRY-RUN ----")
}

func (b *Builder) filterSeen(ctx context.Context, items []news.Item) ([]news.Item, error) {
	out := make([]news.Item, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		h := news.CanonicalURLHash(it.URL)
		if seen[h] {
			continue
		}
		seen[h] = true
		already, err := b.Articles.Seen(ctx, h)
		if err != nil {
			return nil, err
		}
		if !already {
			out = append(out, it)
		}
	}
	return out, nil
}

func (b *Builder) recordArticles(ctx context.Context, arts []news.Item, log *slog.Logger) {
	for _, a := range arts {
		err := b.Articles.Record(ctx, store.Article{
			URLHash: news.CanonicalURLHash(a.URL),
			URL:     a.URL,
			Title:   nullableString(a.Title),
			Source:  nullableString(a.Source),
		})
		if err != nil {
			log.Warn("record article failed", "url", a.URL, "err", err)
		}
	}
}

func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func (b *Builder) recordPostsLog(ctx context.Context, now time.Time, m Message, mid int64, log *slog.Logger) {
	type logPayload struct {
		Date     string   `json:"date"`
		Source   []string `json:"sources"`
		URLs     []string `json:"urls"`
		Messages int      `json:"-"`
	}
	p := logPayload{Date: now.Format(time.RFC3339)}
	for _, a := range m.Articles {
		p.Source = append(p.Source, a.Source)
		p.URLs = append(p.URLs, a.URL)
	}
	payload, _ := json.Marshal(p)
	if _, err := b.Posts.Record(ctx, store.KindDigest, string(payload), mid); err != nil {
		log.Warn("record posts_log failed", "err", err)
	}
}

func itemsToLLMArticles(items []news.Item) []llm.Article {
	out := make([]llm.Article, len(items))
	for i, it := range items {
		out[i] = llm.Article{
			Source:    it.Source,
			Title:     it.Title,
			URL:       it.URL,
			Published: it.Published.Format(time.RFC3339),
			Body:      it.Summary,
		}
	}
	return out
}

func (b *Builder) logger() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return slog.Default()
}

func (b *Builder) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

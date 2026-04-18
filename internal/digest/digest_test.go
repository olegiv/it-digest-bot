package digest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// ---- minimal fakes ---------------------------------------------------------

type fakeFetcher struct {
	items []news.Item
	err   error
}

func (f fakeFetcher) FetchAll(_ context.Context) ([]news.Item, error) {
	return f.items, f.err
}

type fakeSummarizer struct {
	summaries []llm.Summary
	err       error
	calls     int32
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ llm.SummarizeRequest) ([]llm.Summary, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.summaries, f.err
}

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func openStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ---- tests -----------------------------------------------------------------

func TestBuilderHappyPath(t *testing.T) {
	t.Parallel()

	var tgCalls int32
	var lastText string
	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tgCalls, 1)
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		lastText = string(b)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1000}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	items := []news.Item{
		{Source: "OpenAI", Title: "GPT", URL: "https://openai.com/a", Published: time.Now()},
		{Source: "Anthropic", Title: "Claude", URL: "https://anthropic.com/b", Published: time.Now()},
	}
	sum := &fakeSummarizer{summaries: []llm.Summary{
		{SourceIndex: 0, Headline: "A", Blurb: "blurbA"},
		{SourceIndex: 1, Headline: "B", Blurb: "blurbB"},
	}}

	b := &Builder{
		Fetcher:    fakeFetcher{items: items},
		Summarizer: sum,
		Bot: telegram.New("t", telegram.WithBaseURL(tgSrv.URL),
			telegram.WithHTTPClient(httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0)))),
		Channel:   "@c",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 1024,
		Articles:  st.Articles,
		Posts:     st.Posts,
		Now:       func() time.Time { return time.Date(2026, 4, 18, 8, 0, 0, 0, time.UTC) },
	}

	res, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Fetched != 2 || res.NewItems != 2 || res.Summaries != 2 || res.Messages != 1 {
		t.Errorf("result = %+v", res)
	}
	if atomic.LoadInt32(&sum.calls) != 1 {
		t.Errorf("summarizer called %d times", sum.calls)
	}
	if atomic.LoadInt32(&tgCalls) != 1 {
		t.Errorf("telegram called %d times, want 1", tgCalls)
	}
	if !strings.Contains(lastText, "blurbA") || !strings.Contains(lastText, "blurbB") {
		t.Errorf("telegram body missing blurbs: %s", lastText)
	}

	// Articles should be recorded.
	seen, err := st.Articles.Seen(context.Background(), news.CanonicalURLHash("https://openai.com/a"))
	if err != nil || !seen {
		t.Errorf("article not recorded after post; err=%v seen=%v", err, seen)
	}
	n, _ := st.Posts.Count(context.Background(), store.KindDigest)
	if n != 1 {
		t.Errorf("posts_log rows = %d, want 1", n)
	}
}

func TestBuilderDedupes(t *testing.T) {
	t.Parallel()

	var tgCalls int32
	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&tgCalls, 1)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer tgSrv.Close()

	st := openStore(t)
	// Pre-record one of the two URLs as already seen.
	_ = st.Articles.Record(context.Background(), store.Article{
		URLHash: news.CanonicalURLHash("https://openai.com/a"),
		URL:     "https://openai.com/a",
	})

	items := []news.Item{
		{Source: "OpenAI", Title: "A", URL: "https://openai.com/a", Published: time.Now()},
		{Source: "Anthropic", Title: "B", URL: "https://anthropic.com/b", Published: time.Now()},
	}
	sum := &fakeSummarizer{summaries: []llm.Summary{
		{SourceIndex: 0, Headline: "New", Blurb: "blurb"},
	}}

	b := &Builder{
		Fetcher:    fakeFetcher{items: items},
		Summarizer: sum,
		Bot: telegram.New("t", telegram.WithBaseURL(tgSrv.URL),
			telegram.WithHTTPClient(httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0)))),
		Channel:  "@c",
		Articles: st.Articles,
		Posts:    st.Posts,
	}

	res, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.NewItems != 1 {
		t.Errorf("NewItems = %d, want 1 (the already-seen one should be filtered)", res.NewItems)
	}
}

func TestBuilderNoItemsExitsCleanly(t *testing.T) {
	t.Parallel()
	st := openStore(t)

	b := &Builder{
		Fetcher:    fakeFetcher{items: nil},
		Summarizer: &fakeSummarizer{},
		Articles:   st.Articles,
		Posts:      st.Posts,
	}
	res, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Summaries != 0 || res.Messages != 0 {
		t.Errorf("result = %+v", res)
	}
}

func TestBuilderPropagatesSummarizerError(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	b := &Builder{
		Fetcher:    fakeFetcher{items: []news.Item{{Source: "s", URL: "u", Published: time.Now()}}},
		Summarizer: &fakeSummarizer{err: errors.New("boom")},
		Articles:   st.Articles,
		Posts:      st.Posts,
	}
	_, err := b.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected 'boom' error, got %v", err)
	}
}

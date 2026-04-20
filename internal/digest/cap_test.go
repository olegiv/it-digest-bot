package digest

import (
	"testing"

	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
)

func TestCapPerSource_DropsOverflow(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "Anthropic", URL: "https://a/1"},
		{Source: "Anthropic", URL: "https://a/2"},
		{Source: "Anthropic", URL: "https://a/3"},
		{Source: "Anthropic", URL: "https://a/4"},
		{Source: "OpenAI", URL: "https://o/1"},
	}
	// Model returned 4 Anthropic + 1 OpenAI, ordered by importance.
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "A1"},
		{SourceIndex: 1, Headline: "A2"},
		{SourceIndex: 2, Headline: "A3"},
		{SourceIndex: 3, Headline: "A4"},
		{SourceIndex: 4, Headline: "O1"},
	}

	got := capPerSource(summaries, items, 2, nil)

	if len(got) != 3 {
		t.Fatalf("got %d summaries, want 3 (2 Anthropic + 1 OpenAI)", len(got))
	}
	// Top-ranked Anthropic items survive; overflow is dropped.
	if got[0].Headline != "A1" || got[1].Headline != "A2" || got[2].Headline != "O1" {
		t.Errorf("unexpected order/selection: %+v", got)
	}
}

func TestCapPerSource_PreservesOrderAcrossSources(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "A", URL: "https://a/1"},
		{Source: "B", URL: "https://b/1"},
		{Source: "A", URL: "https://a/2"},
		{Source: "B", URL: "https://b/2"},
		{Source: "A", URL: "https://a/3"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "A1"},
		{SourceIndex: 1, Headline: "B1"},
		{SourceIndex: 2, Headline: "A2"},
		{SourceIndex: 3, Headline: "B2"},
		{SourceIndex: 4, Headline: "A3"}, // dropped: A already at cap 2
	}

	got := capPerSource(summaries, items, 2, nil)

	want := []string{"A1", "B1", "A2", "B2"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d", len(got), len(want))
	}
	for i, s := range got {
		if s.Headline != want[i] {
			t.Errorf("index %d: got %q, want %q", i, s.Headline, want[i])
		}
	}
}

func TestCapPerSource_DisabledWhenNonPositive(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "A", URL: "https://a/1"},
		{Source: "A", URL: "https://a/2"},
		{Source: "A", URL: "https://a/3"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "A1"},
		{SourceIndex: 1, Headline: "A2"},
		{SourceIndex: 2, Headline: "A3"},
	}
	for _, cap := range []int{0, -1, -99} {
		got := capPerSource(summaries, items, cap, nil)
		if len(got) != len(summaries) {
			t.Errorf("cap=%d: got %d, want %d (passthrough)", cap, len(got), len(summaries))
		}
	}
}

func TestCapPerSource_InvalidIndexPassesThrough(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "A", URL: "https://a/1"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "A1"},
		{SourceIndex: 99, Headline: "bad"},
		{SourceIndex: -1, Headline: "bad2"},
		{SourceIndex: 0, Headline: "A2"}, // dropped: A at cap 1
	}

	got := capPerSource(summaries, items, 1, nil)

	// Invalid indices are not counted against the cap and are left for
	// render.go to skip — so they flow through. A2 is dropped because A
	// already hit its cap via A1.
	want := []string{"A1", "bad", "bad2"}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i, s := range got {
		if s.Headline != want[i] {
			t.Errorf("index %d: got %q, want %q", i, s.Headline, want[i])
		}
	}
}

func TestCapPerSource_EmptyInput(t *testing.T) {
	t.Parallel()
	if got := capPerSource(nil, nil, 2, nil); got != nil {
		t.Errorf("nil input should return nil, got %+v", got)
	}
	if got := capPerSource([]llm.Summary{}, []news.Item{}, 2, nil); len(got) != 0 {
		t.Errorf("empty input should return empty, got %+v", got)
	}
}

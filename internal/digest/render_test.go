package digest

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
)

var testDate = time.Date(2026, 4, 18, 8, 0, 0, 0, time.UTC)

func TestRenderSingleMessage(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "OpenAI", Title: "GPT", URL: "https://openai.com/x"},
		{Source: "Anthropic", Title: "Claude", URL: "https://anthropic.com/y"},
		{Source: "OpenAI", Title: "DALL-E", URL: "https://openai.com/z"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "GPT update", Blurb: "A new model."},
		{SourceIndex: 1, Headline: "Claude update", Blurb: "Another model."},
		{SourceIndex: 2, Headline: "DALL-E update", Blurb: "Image model."},
	}

	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	text := msgs[0].Text
	for _, want := range []string{
		"🗞 *Daily AI Digest*",
		"April 18",
		"*OpenAI*",
		"*Anthropic*",
		"GPT update",
		"Claude update",
		"DALL\\-E update",
		"\\#AI",
		"\\#DailyDigest",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if len(msgs[0].Articles) != 3 {
		t.Errorf("articles = %d, want 3", len(msgs[0].Articles))
	}
}

func TestRenderGroupsBySource(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "A", Title: "t1", URL: "https://a1"},
		{Source: "B", Title: "t2", URL: "https://b1"},
		{Source: "A", Title: "t3", URL: "https://a2"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "h1", Blurb: "b1"},
		{SourceIndex: 1, Headline: "h2", Blurb: "b2"},
		{SourceIndex: 2, Headline: "h3", Blurb: "b3"},
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("RenderAndSplit returned no messages")
	}
	text := msgs[0].Text

	aIdx := strings.Index(text, "*A*")
	bIdx := strings.Index(text, "*B*")
	h1Idx := strings.Index(text, "h1")
	h3Idx := strings.Index(text, "h3")

	if aIdx < 0 || bIdx < 0 {
		t.Fatalf("source headers missing: %s", text)
	}
	if aIdx >= h1Idx || h1Idx >= h3Idx || h3Idx >= bIdx {
		t.Errorf("A-group items not before B-group:\n%s", text)
	}
}

func TestRenderSplitsOnLimit(t *testing.T) {
	t.Parallel()
	var items []news.Item
	var summaries []llm.Summary
	for i := range 6 {
		items = append(items, news.Item{
			Source: string(rune('A' + i)),
			Title:  strings.Repeat("x", 40),
			URL:    "https://example.com/" + string(rune('a'+i)),
		})
		summaries = append(summaries, llm.Summary{
			SourceIndex: i,
			Headline:    strings.Repeat("H", 90),
			Blurb:       strings.Repeat("b", 250),
		})
	}

	msgs, err := RenderAndSplit(testDate, items, summaries, 500, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected multiple messages, got %d", len(msgs))
	}
	for _, m := range msgs {
		if len(m.Text) > 500 {
			t.Errorf("message too long: %d bytes", len(m.Text))
		}
	}
	for _, m := range msgs {
		if !strings.Contains(m.Text, "🗞") {
			t.Errorf("continuation missing header:\n%s", m.Text)
		}
		if !strings.Contains(m.Text, "\\#AI") {
			t.Errorf("continuation missing footer:\n%s", m.Text)
		}
	}
	// Every message should have a "\(i/N\)" MarkdownV2-escaped page counter.
	for i, m := range msgs {
		want := `\(` + string(rune('0'+i+1)) + "/" + string(rune('0'+len(msgs))) + `\)`
		if !strings.Contains(m.Text, want) {
			t.Errorf("message %d missing page counter %q:\n%s", i+1, want, m.Text)
		}
	}
}

func TestRenderSkipsInvalidSummaryIndex(t *testing.T) {
	t.Parallel()
	items := []news.Item{{Source: "A", Title: "t", URL: "https://a"}}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "ok", Blurb: "ok"},
		{SourceIndex: 99, Headline: "bad", Blurb: "should be skipped"},
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("RenderAndSplit returned no messages")
	}
	if strings.Contains(msgs[0].Text, "should be skipped") {
		t.Errorf("invalid summary index was rendered:\n%s", msgs[0].Text)
	}
}

func TestRenderEmptyReturnsNoMessages(t *testing.T) {
	t.Parallel()
	msgs, err := RenderAndSplit(testDate, nil, nil, 4096, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("got %d messages, want 0 for empty input", len(msgs))
	}
}

// TestRenderTreatsFeedAndModelTextAsPlain: feed titles and model output are
// untrusted (a feed item can carry a prompt injection), so links, code spans
// and formatting in them must come out inert. The only live link per entry is
// the article URL.
func TestRenderTreatsFeedAndModelTextAsPlain(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "Feed [x](https://evil.example)", Title: "t", URL: "https://example.com/a"},
		{Source: "Feed", Title: "*bold* [phish](https://evil.example)", URL: "https://example.com/b"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "Read `this` [now](https://evil.example)", Blurb: "Click [here](tg://resolve?domain=evil) ||spoiler|| _it_"},
		{SourceIndex: 1, Headline: "", Blurb: ""},
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("RenderAndSplit: %d messages, %v", len(msgs), err)
	}
	text := msgs[0].Text
	for _, bad := range []string{"[x](", "[now](", "[here](", "[phish](", "`this`", "||spoiler||", "*bold*"} {
		if strings.Contains(text, bad) {
			t.Errorf("live markup %q leaked:\n%s", bad, text)
		}
	}
	for _, want := range []string{
		"*Feed \\[x\\]\\(https://evil\\.example\\)*\n",
		"• [Read \\`this\\` \\[now\\]\\(https://evil\\.example\\)](https://example.com/a) — Click \\[here\\]",
		"• [\\*bold\\* \\[phish\\]\\(https://evil\\.example\\)](https://example.com/b)\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Count(text, "](https://example.com/") != 2 || strings.Count(text, "](") != 2 {
		t.Errorf("want exactly the two article links:\n%s", text)
	}
}

func TestRenderLinksOnlyHTTPArticleURLs(t *testing.T) {
	t.Parallel()
	urls := []string{
		"https://example.com/ok",
		"http://example.com/ok",
		"javascript:alert(1)",
		"tg://resolve?domain=evil",
		"/relative/path",
		"https:///no-host",
		"data:text/html,x",
		"%zz",
	}
	items := make([]news.Item, len(urls))
	summaries := make([]llm.Summary, len(urls))
	for i, u := range urls {
		items[i] = news.Item{Source: "S", Title: "t", URL: u}
		summaries[i] = llm.Summary{SourceIndex: i, Headline: "h" + string(rune('a'+i)), Blurb: "b"}
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("RenderAndSplit: %d messages, %v", len(msgs), err)
	}
	text := msgs[0].Text
	if !strings.Contains(text, "• [ha](https://example.com/ok) — b") || !strings.Contains(text, "• [hb](http://example.com/ok) — b") {
		t.Errorf("http(s) URLs must stay linked:\n%s", text)
	}
	for i := 2; i < len(urls); i++ {
		want := "• h" + string(rune('a'+i)) + " — b\n"
		if !strings.Contains(text, want) {
			t.Errorf("%q must render unlinked (%q):\n%s", urls[i], want, text)
		}
	}
	if len(msgs[0].Articles) != len(urls) {
		t.Errorf("unlinked entries must still be recorded as posted: %d", len(msgs[0].Articles))
	}
}

func TestRenderStripsControlAndFormatRunes(t *testing.T) {
	t.Parallel()
	items := []news.Item{{Source: "S\u202e", Title: "t", URL: "https://example.com/a"}}
	summaries := []llm.Summary{{SourceIndex: 0, Headline: "Big\x1b[2J news\u200b", Blurb: "Line\none\x00two\u202e"}}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("RenderAndSplit: %d messages, %v", len(msgs), err)
	}
	text := msgs[0].Text
	if !strings.Contains(text, "• [Big \\[2J news](https://example.com/a) — Line one two\n") {
		t.Errorf("unexpected entry:\n%q", text)
	}
	for _, r := range text {
		if (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("digest contains control/format rune %U:\n%q", r, text)
		}
	}
}

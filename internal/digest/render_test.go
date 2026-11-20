package digest

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
	"github.com/olegiv/it-digest-bot/internal/telegram"
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

// TestRenderKeepsOnlySameHostLinks: feed items and the model's summaries of
// them are untrusted, so a link in a blurb stays live only when it points at
// the article's own host. Other links keep their text; bare URLs to other
// hosts (which Telegram would auto-link) are dropped. Code spans and other
// markup keep working as before.
func TestRenderKeepsOnlySameHostLinks(t *testing.T) {
	t.Parallel()
	items := []news.Item{{Source: "OpenAI", Title: "t", URL: "https://www.openai.com/news/a"}}
	summaries := []llm.Summary{{
		SourceIndex: 0,
		Headline:    "New `gpt` [model](https://openai.com/x)",
		Blurb: "See [docs](https://openai.com/docs), [blog](http://WWW.OpenAI.com./blog) and `code`; " +
			"[phish](https://evil.example/login), [sub](https://evil.openai.com/x), " +
			"[trick](https://openai.com@evil.example/), [js](javascript:alert(1)), " +
			"[rel](/relative). Bare https://evil.example/x, kept https://openai.com/y.",
	}}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("RenderAndSplit: %d messages, %v", len(msgs), err)
	}
	text := msgs[0].Text
	for _, want := range []string{
		// Headline sits inside the article link: its own link becomes text, code stays.
		"• [New `gpt` model](https://www.openai.com/news/a) — ",
		"See [docs](https://openai.com/docs), [blog](http://WWW.OpenAI.com./blog) and `code`; ",
		// The link grammar ends at the first ')', so "javascript:alert(1)"
		// leaves a literal, escaped ')'.
		"phish, sub, trick, js\\), rel\\. Bare , kept https://openai\\.com/y\\.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, bad := range []string{"evil", "javascript", "/relative"} {
		if strings.Contains(text, bad) {
			t.Errorf("off-host target %q leaked:\n%s", bad, text)
		}
	}
	if n := strings.Count(text, "]("); n != 3 {
		t.Errorf("want 3 live links (article, docs, blog), got %d:\n%s", n, text)
	}
}

func TestRenderAllowsNoBlurbLinksWithoutArticleURL(t *testing.T) {
	t.Parallel()
	items := []news.Item{{Source: "S", Title: "t", URL: "javascript:alert(1)"}}
	summaries := []llm.Summary{{SourceIndex: 0, Headline: "H [x](https://a.example/)", Blurb: "[y](https://a.example/) https://a.example/z"}}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("RenderAndSplit: %d messages, %v", len(msgs), err)
	}
	if !strings.Contains(msgs[0].Text, "• H x — y \n") || strings.Contains(msgs[0].Text, "](") {
		t.Errorf("no link may survive without a valid article URL:\n%s", msgs[0].Text)
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

// TestRenderBoundsOversizedModelOutput: the 280-character limit is only in
// the prompt. A prompt injection that makes the model write kilobytes per
// entry must not produce a message Telegram rejects as too long.
func TestRenderBoundsOversizedModelOutput(t *testing.T) {
	t.Parallel()
	var items []news.Item
	var summaries []llm.Summary
	for i := range 3 {
		items = append(items, news.Item{Source: "Same", Title: "t", URL: "https://example.com/" + string(rune('a'+i))})
		summaries = append(summaries, llm.Summary{
			SourceIndex: i,
			Headline:    strings.Repeat("H.", 3000),
			Blurb:       strings.Repeat("é.[x](y) ", 2000),
		})
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	articles := 0
	for i, m := range msgs {
		if len(m.Text) > 4096 {
			t.Errorf("message %d is %d bytes", i+1, len(m.Text))
		}
		if !strings.Contains(m.Text, "*Same*\n") {
			t.Errorf("message %d lacks the repeated source line:\n%s", i+1, m.Text)
		}
		articles += len(m.Articles)
	}
	if articles != 3 {
		t.Errorf("articles = %d, want all 3 entries kept (capped, not dropped)", articles)
	}
	if !strings.Contains(msgs[0].Text, "…") {
		t.Error("capped text must end with an ellipsis")
	}
}

// TestRenderSplitsLargeGroupByEntry: one source whose entries together
// exceed a message is split across messages, each repeating the source.
func TestRenderSplitsLargeGroupByEntry(t *testing.T) {
	t.Parallel()
	var items []news.Item
	var summaries []llm.Summary
	for i := range 4 {
		items = append(items, news.Item{Source: "Big", Title: "t", URL: "https://example.com/" + string(rune('a'+i))})
		summaries = append(summaries, llm.Summary{SourceIndex: i, Headline: "h", Blurb: strings.Repeat("b", 200)})
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 600, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) < 2 {
		t.Fatalf("messages = %d, want a split", len(msgs))
	}
	articles := 0
	for i, m := range msgs {
		if len(m.Text) > 600 {
			t.Errorf("message %d is %d bytes", i+1, len(m.Text))
		}
		if strings.Count(m.Text, "*Big*\n") != 1 {
			t.Errorf("message %d should carry the source line once:\n%s", i+1, m.Text)
		}
		articles += len(m.Articles)
	}
	if articles != 4 {
		t.Errorf("articles = %d, want 4", articles)
	}
}

// TestRenderSkipsEntryThatCannotFit: with a tiny limit an entry that cannot
// fit even alone is skipped (and not recorded) instead of failing the send.
func TestRenderSkipsEntryThatCannotFit(t *testing.T) {
	t.Parallel()
	items := []news.Item{
		{Source: "S", Title: "t", URL: "https://example.com/short"},
		{Source: "T", Title: "t", URL: "https://example.com/long"},
	}
	summaries := []llm.Summary{
		{SourceIndex: 0, Headline: "ok", Blurb: "fits"},
		{SourceIndex: 1, Headline: "too long", Blurb: strings.Repeat("x", 600)},
	}
	msgs, err := RenderAndSplit(testDate, items, summaries, 300, nil)
	if err != nil {
		t.Fatalf("RenderAndSplit: %v", err)
	}
	if len(msgs) != 1 || len(msgs[0].Articles) != 1 || msgs[0].Articles[0].URL != "https://example.com/short" {
		t.Fatalf("want only the short entry posted, got %d messages: %+v", len(msgs), msgs)
	}
}

// TestRenderEntriesAreWellFormed is the MD-001 regression at digest level:
// content removed by the link policy must never leave malformed MarkdownV2.
func TestRenderEntriesAreWellFormed(t *testing.T) {
	t.Parallel()
	for _, blurb := range []string{
		"``https://evil.com`x`",
		"``[https://evil.com](https://evil.com)`x`",
		"_https://evil.com/x_ and tg://resolve?domain=evil",
		"[a [b](https://example.com/1)](https://example.com/2)",
	} {
		for _, rawURL := range []string{"https://example.com/a", "javascript:x"} {
			items := []news.Item{{Source: "S", Title: "t", URL: rawURL}}
			summaries := []llm.Summary{{SourceIndex: 0, Headline: blurb, Blurb: blurb}}
			msgs, err := RenderAndSplit(testDate, items, summaries, 4096, nil)
			if err != nil || len(msgs) != 1 {
				t.Fatalf("RenderAndSplit: %d messages, %v", len(msgs), err)
			}
			entry := msgs[0].Text[strings.Index(msgs[0].Text, "• "):strings.LastIndex(msgs[0].Text, "\n\n")]
			if !telegram.WellFormedMarkdownV2(entry) {
				t.Errorf("url %s blurb %q: entry not well formed: %q", rawURL, blurb, entry)
			}
			// Off-host link targets must be gone from the blurb. Inside the
			// headline, which is itself the article link, URL-looking text
			// is not auto-linked and is kept as text.
			_, blurbPart, found := strings.Cut(entry, " — ")
			if !found {
				t.Fatalf("entry without blurb separator: %q", entry)
			}
			for _, bad := range []string{"evil", "tg:"} {
				if strings.Contains(blurbPart, bad) {
					t.Errorf("url %s blurb %q: %q leaked: %q", rawURL, blurb, bad, blurbPart)
				}
			}
		}
	}
}

func TestRenderEntryKeepsFormattingWhenWellFormed(t *testing.T) {
	t.Parallel()
	item := news.Item{Source: "S", Title: "t", URL: "https://example.com/a"}
	got := renderEntry(item, llm.Summary{Headline: "fine `code`", Blurb: "see [docs](https://example.com/d)"}, nil)
	want := "• [fine `code`](https://example.com/a) — see [docs](https://example.com/d)\n"
	if got != want {
		t.Errorf("renderEntry = %q, want %q", got, want)
	}
}

func TestTruncateText(t *testing.T) {
	t.Parallel()
	if got := truncateText("short", 10); got != "short" {
		t.Errorf("no-op: %q", got)
	}
	if got := truncateText("abcdef ghij", 9); got != "abcdef…" {
		t.Errorf("cut: %q", got)
	}
	if got := truncateText("ééééé", 6); got != "é…" {
		t.Errorf("rune boundary: %q", got)
	}
}

func TestSameHostPolicyPorts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		article, link string
		want          bool
	}{
		{"https://good.com/a", "https://good.com/b", true},
		{"https://good.com/a", "https://good.com:443/b", true},
		{"https://good.com/a", "http://good.com:80/b", true},
		{"https://good.com/a", "http://good.com/b", true},
		{"https://good.com/a", "https://good.com:8443/phish", false},
		{"https://good.com/a", "http://good.com:443/b", false},
		{"https://good.com:8443/a", "https://good.com:8443/b", true},
		{"https://good.com:8443/a", "https://good.com/b", false},
		{"https://www.good.com/a", "https://good.com./b", true},
		{"https://good.com/a", "https://evil.good.com/b", false},
	} {
		u, ok := articleURL(tc.article)
		if !ok {
			t.Fatalf("articleURL(%q) rejected", tc.article)
		}
		if got := sameHost(u)(tc.link); got != tc.want {
			t.Errorf("sameHost(%s)(%s) = %v, want %v", tc.article, tc.link, got, tc.want)
		}
	}
}

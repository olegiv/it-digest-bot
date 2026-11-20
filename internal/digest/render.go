package digest

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
	"github.com/olegiv/it-digest-bot/internal/strs"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// Message is one Telegram-sized chunk of a digest, ready to send, along with
// the articles it mentions so the caller can record them after the send
// succeeds.
type Message struct {
	Text     string
	Articles []news.Item
}

// RenderAndSplit turns the model's ranked summaries into one or more
// MarkdownV2 messages, each under maxBytes. Groups are source-grouped;
// each group is kept intact within a single message.
func RenderAndSplit(
	date time.Time,
	items []news.Item,
	summaries []llm.Summary,
	maxBytes int,
	log *slog.Logger,
) ([]Message, error) {
	groups := buildGroups(items, summaries, log)
	if len(groups) == 0 {
		return nil, nil
	}

	header := fmt.Sprintf("🗞 *Daily AI Digest* — %s\n\n",
		telegram.EscapeMarkdownV2(date.Format("January 2")))
	footer := "\n\n\\#AI \\#DailyDigest"

	return packGroups(header, footer, groups, maxBytes, log), nil
}

// Length caps on model and feed text, in bytes of cleaned text before
// escaping (escaping at most doubles them). The prompt asks for short
// headlines and blurbs, but the model's output is shaped by untrusted feed
// content, so the renderer enforces the bound itself: together with
// maxArticleURLBytes it keeps any single entry far below
// telegram.MaxMessageBytes.
const (
	maxHeadlineBytes   = 300
	maxBlurbBytes      = 700
	maxArticleURLBytes = 512

	// pageCounterReserve leaves room for the " \(i/n\)" page counter added
	// to every message once a digest is split.
	pageCounterReserve = 16
)

type group struct {
	source  string // rendered "*Source*\n" line
	entries []renderedEntry
}

type renderedEntry struct {
	text string // one rendered "• …\n" line
	item news.Item
}

func (g group) size() int {
	n := len(g.source)
	for _, e := range g.entries {
		n += len(e.text)
	}
	return n
}

func buildGroups(items []news.Item, summaries []llm.Summary, log *slog.Logger) []group {
	// Group summaries by source, preserving model ranking order.
	type entry struct {
		item    news.Item
		summary llm.Summary
	}
	bySource := map[string][]entry{}
	sourceOrder := []string{}

	for _, s := range summaries {
		if s.SourceIndex < 0 || s.SourceIndex >= len(items) {
			if log != nil {
				log.Warn("summary references invalid source index",
					"source_index", s.SourceIndex, "total_items", len(items))
			}
			continue
		}
		it := items[s.SourceIndex]
		if _, ok := bySource[it.Source]; !ok {
			sourceOrder = append(sourceOrder, it.Source)
		}
		bySource[it.Source] = append(bySource[it.Source], entry{item: it, summary: s})
	}

	out := make([]group, 0, len(sourceOrder))
	for _, src := range sourceOrder {
		name := telegram.EscapeMarkdownV2(telegram.CleanText(src))
		if !telegram.WellFormedMarkdownV2(name) {
			name = telegram.EscapeMarkdownV2Plain(telegram.CleanText(src))
		}
		g := group{source: "*" + name + "*\n"}
		for _, e := range bySource[src] {
			g.entries = append(g.entries, renderedEntry{text: renderEntry(e.item, e.summary, log), item: e.item})
		}
		out = append(out, g)
	}
	return out
}

// renderEntry renders one "• [headline](url) — blurb" line. Headline and
// blurb keep their formatting under the same-host link policy (see
// sameHost). If the result is not well-formed MarkdownV2, which would make
// Telegram reject the whole message, the entry falls back to plain text.
func renderEntry(item news.Item, sum llm.Summary, log *slog.Logger) string {
	headline := truncateText(strs.FirstNonEmpty(
		telegram.CleanText(sum.Headline), telegram.CleanText(item.Title)), maxHeadlineBytes)
	blurb := truncateText(telegram.CleanText(sum.Blurb), maxBlurbBytes)
	u, linked := articleURL(item.URL)

	format := func(headline, blurb string) string {
		if linked {
			return "• [" + headline + "](" + telegram.EscapeMarkdownV2URL(strings.TrimSpace(item.URL)) + ") — " + blurb + "\n"
		}
		return "• " + headline + " — " + blurb + "\n"
	}
	var text string
	if linked {
		text = format(telegram.EscapeMarkdownV2LinkText(headline), telegram.EscapeMarkdownV2Links(blurb, sameHost(u)))
	} else {
		text = format(telegram.EscapeMarkdownV2Links(headline, nil), telegram.EscapeMarkdownV2Links(blurb, nil))
	}
	if telegram.WellFormedMarkdownV2(text) {
		return text
	}
	if log != nil {
		log.Warn("digest entry is not well-formed MarkdownV2; posting it as plain text",
			"source", item.Source, "url", item.URL)
	}
	return format(telegram.EscapeMarkdownV2Plain(headline), telegram.EscapeMarkdownV2Plain(blurb))
}

// truncateText cuts s to at most n bytes on a rune boundary, ending in "…"
// when it was shortened. A link or code span cut in half is no longer
// matched by the escapers and comes out as escaped literal text.
func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const ellipsis = "…"
	cut := n - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ") + ellipsis
}

// articleURL parses the item URL and accepts it only as an absolute
// http(s) URL with a host and no userinfo, at most maxArticleURLBytes long.
// Anything else (a relative path, javascript:, tg:, data:, an oversized
// URL …) leaves the headline unlinked and allows no links in the blurb.
func articleURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxArticleURLBytes {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil || !isWebURL(u) {
		return nil, false
	}
	return u, true
}

// sameHost returns the digest's link policy for one entry: feed items and
// the model's summaries of them are untrusted (a prompt injection in a feed
// item could make the model emit any link), so a link in the blurb stays
// live only when it is an http(s) URL on the article's own host. "www." is
// ignored on both sides; other subdomains count as different hosts. With
// no valid article URL every link is rejected.
func sameHost(article *url.URL) func(string) bool {
	if article == nil {
		return nil
	}
	want := canonicalHost(article)
	return func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && isWebURL(u) && canonicalHost(u) == want
	}
}

func isWebURL(u *url.URL) bool {
	return (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil
}

func canonicalHost(u *url.URL) string {
	return strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), "www.")
}

func packGroups(header, footer string, groups []group, maxBytes int, log *slog.Logger) []Message {
	if maxBytes <= 0 {
		maxBytes = telegram.MaxMessageBytes
	}
	budget := maxBytes - len(footer) - pageCounterReserve

	var messages []Message
	var cur strings.Builder
	var curArticles []news.Item
	cur.WriteString(header)

	hasContent := func() bool { return cur.Len() > len(header) }
	room := func(n int) bool { return cur.Len()+n <= budget }
	flush := func() {
		if !hasContent() {
			return
		}
		messages = append(messages, Message{
			Text:     strings.TrimRight(cur.String(), "\n") + footer,
			Articles: curArticles,
		})
		cur.Reset()
		cur.WriteString(header)
		curArticles = nil
	}

	for _, g := range groups {
		size := g.size() + 1 // blank line after the group
		if !room(size) && hasContent() {
			flush()
		}
		if room(size) {
			cur.WriteString(g.source)
			for _, e := range g.entries {
				cur.WriteString(e.text)
				curArticles = append(curArticles, e.item)
			}
			cur.WriteByte('\n')
			continue
		}

		// The group alone does not fit one message: split it by entry and
		// repeat the source line in each message it continues in.
		open := false
		for _, e := range g.entries {
			need := len(e.text)
			if !open {
				need += len(g.source)
			}
			if !room(need) && hasContent() {
				flush()
				open = false
				need = len(g.source) + len(e.text)
			}
			if !room(need) {
				if log != nil {
					log.Warn("digest entry does not fit one message; skipped",
						"source", e.item.Source, "url", e.item.URL, "bytes", len(e.text))
				}
				continue
			}
			if !open {
				cur.WriteString(g.source)
				open = true
			}
			cur.WriteString(e.text)
			curArticles = append(curArticles, e.item)
		}
		if open {
			cur.WriteByte('\n')
		}
	}
	flush()

	if len(messages) > 1 {
		for i := range messages {
			messages[i].Text = addPageCounter(messages[i].Text, i+1, len(messages))
		}
	}
	return messages
}

// addPageCounter inserts "(i/n)" immediately after the date in the header.
// Header is "🗞 *Daily AI Digest* — <escaped date>\n\n...".
func addPageCounter(msg string, i, n int) string {
	headerEnd := strings.Index(msg, "\n\n")
	if headerEnd < 0 {
		return msg
	}
	counter := fmt.Sprintf(" \\(%d/%d\\)", i, n)
	return msg[:headerEnd] + counter + msg[headerEnd:]
}

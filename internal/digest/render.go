package digest

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

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

	return packGroups(header, footer, groups, maxBytes), nil
}

type group struct {
	source   string
	body     string
	articles []news.Item
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
		entries := bySource[src]
		var b strings.Builder
		fmt.Fprintf(&b, "*%s*\n", telegram.EscapeMarkdownV2(src))
		articles := make([]news.Item, 0, len(entries))
		for _, e := range entries {
			headline := strs.FirstNonEmpty(e.summary.Headline, e.item.Title)
			fmt.Fprintf(&b, "• [%s](%s) — %s\n",
				telegram.EscapeMarkdownV2(headline),
				telegram.EscapeMarkdownV2URL(e.item.URL),
				telegram.EscapeMarkdownV2(e.summary.Blurb))
			articles = append(articles, e.item)
		}
		out = append(out, group{source: src, body: b.String(), articles: articles})
	}
	return out
}

func packGroups(header, footer string, groups []group, maxBytes int) []Message {
	if maxBytes <= 0 {
		maxBytes = telegram.MaxMessageBytes
	}

	var messages []Message
	var cur strings.Builder
	var curArticles []news.Item
	cur.WriteString(header)

	flush := func() {
		if cur.Len() <= len(header) {
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
		prospective := cur.Len() + len(g.body) + len(footer) + 1
		if cur.Len() > len(header) && prospective > maxBytes {
			flush()
		}
		cur.WriteString(g.body)
		cur.WriteByte('\n')
		curArticles = append(curArticles, g.articles...)
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

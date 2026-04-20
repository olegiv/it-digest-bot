package digest

import (
	"log/slog"

	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
)

// capPerSource keeps at most maxPerSource summaries per source, walking
// the input in order so the LLM's ranking decides which ones survive.
// When maxPerSource <= 0 the input is returned unchanged.
//
// Summaries whose SourceIndex is out of range are passed through and left
// for the render layer to skip — it already logs invalid indices, and
// double-logging them here would just be noise.
func capPerSource(summaries []llm.Summary, items []news.Item, maxPerSource int, log *slog.Logger) []llm.Summary {
	if maxPerSource <= 0 || len(summaries) == 0 {
		return summaries
	}
	counts := make(map[string]int)
	out := make([]llm.Summary, 0, len(summaries))
	var dropped int
	for _, s := range summaries {
		if s.SourceIndex < 0 || s.SourceIndex >= len(items) {
			out = append(out, s)
			continue
		}
		src := items[s.SourceIndex].Source
		if counts[src] >= maxPerSource {
			dropped++
			continue
		}
		counts[src]++
		out = append(out, s)
	}
	if dropped > 0 && log != nil {
		log.Info("capped per-source items",
			"max_per_source", maxPerSource,
			"dropped", dropped,
			"kept", len(out))
	}
	return out
}

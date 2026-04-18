// Package llm provides an Anthropic Claude API client for the daily
// digest. Implemented directly against POST /v1/messages via net/http —
// no SDK, as specified.
package llm

import "context"

// SummarizeRequest describes a single summarize call.
type SummarizeRequest struct {
	Model     string
	MaxTokens int
	Articles  []Article
}

// Article is an item the model is asked to rank and summarize.
type Article struct {
	Source    string
	Title     string
	URL       string
	Published string
	Body      string
}

// Summary is a single model-ranked output entry.
type Summary struct {
	SourceIndex int
	Headline    string
	Blurb       string
}

// Summarizer produces a ranked, summarized view of a set of articles.
type Summarizer interface {
	Summarize(ctx context.Context, req SummarizeRequest) ([]Summary, error)
}

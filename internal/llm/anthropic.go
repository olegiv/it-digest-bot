package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/strs"
)

// AnthropicClient calls POST /v1/messages directly (no SDK) as specified.
//
// The daily digest runs once every 24h, so neither the 5-minute nor the
// 1-hour prompt cache TTL pays off — cache_control is deliberately omitted.
type AnthropicClient struct {
	apiKey  string
	model   string
	baseURL string
	http    *httpx.Client
}

// AnthropicOption configures an AnthropicClient.
type AnthropicOption func(*AnthropicClient)

// WithAnthropicBaseURL overrides the API base URL (for tests).
func WithAnthropicBaseURL(u string) AnthropicOption {
	return func(c *AnthropicClient) { c.baseURL = u }
}

// NewAnthropic returns a client bound to the given API key + model.
func NewAnthropic(apiKey, model string, h *httpx.Client, opts ...AnthropicOption) *AnthropicClient {
	c := &AnthropicClient{
		apiKey:  apiKey,
		model:   model,
		baseURL: "https://api.anthropic.com",
		http:    h,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

const systemPromptTemplate = `You are an AI news curator for a daily Telegram digest. The audience is working software engineers and ML practitioners — people who ship code. They care about things that change what they can build or how they build it.

You will receive a JSON array of article candidates from various AI-focused feeds. Each candidate has an "index" field.

STRONGLY PREFER (include if substantive):
- New foundation-model releases or version bumps from major labs
- New developer tools and IDE integrations (Copilot, Codex, Cursor, Windsurf, Aider, JetBrains AI, Zed AI, etc.)
- New APIs, SDKs, endpoint features, or pricing changes from major AI providers
- Open-source model releases with published weights
- New inference or training runtimes and local-AI clients (vLLM, SGLang, TGI, llama.cpp, Ollama, LM Studio, TensorRT-LLM, etc.)
- Major research with concrete engineering implications (architectures, training recipes, agent designs)
- Performance milestones on widely-used benchmarks (SWE-bench, HumanEval, MMLU, MATH, GPQA, AIME, etc.)
- Agent frameworks, tool-use, and MCP (Model Context Protocol) ecosystem updates

SOURCE PRIORITY when ranking items of comparable substance:
1. Anthropic — new Claude models, Anthropic Labs launches, new Anthropic APIs, Anthropic research papers
2. OpenAI — new GPT / o-series models, Codex, ChatGPT features, new OpenAI APIs
3. Google/DeepMind, Meta, Mistral, xAI, DeepSeek, Qwen and other major model labs; plus widely-used local-AI runtimes and clients when they are the subject of the item — Ollama, LM Studio, llama.cpp, vLLM, SGLang, TGI
4. Developer-community sources — Hacker News technical posts, open-source projects, independent research, arXiv
5. Editorial aggregators (e.g. The Batch) — typically business / industry framing, useful but ranked last

A blockbuster release from a lower-tier source (e.g. a state-of-the-art open-weights model) can outrank a minor Anthropic or OpenAI item — use judgment. But all else equal, rank by source tier. Put the highest-tier item first so the digest opens with the most brand-authoritative news.

STRONGLY DEPRIORITIZE (usually skip):
- Government policy, regulation, or political commentary
- Fundraising rounds, M&A, valuations, corporate dealmaking
- CEO interviews, opinion essays, or hot takes without technical content
- Generic "AI will change X industry" business pieces
- Hype pieces without a concrete release, paper, or measurable result

Pick the top items for this audience. Target 5 to 8 items total; if fewer qualify, return fewer. Always include at least one item when ANY candidate is even tangentially engineering-relevant — an empty digest should only happen when every candidate is purely policy, fundraising, or general-business news. Don't be afraid to include borderline items that mention a release, paper, benchmark, or technical decision; your job is to surface signal for builders, not to be a strict gatekeeper.

DIVERSITY — HARD CAP: Return at most %d items from any single source (identified by the "source" field in the input). Even if one source has many strong candidates, pick its top %d and drop the rest; a slightly less important item from an uncovered source is better than a third item from a source already represented. This cap is non-negotiable and applies even on a big-news day for a single provider.

Output: ONLY a JSON array with this exact shape. No prose before or after. No code fences.
[
  {"source_index": <int — the "index" of the chosen article>,
   "headline": "<concise rewrite of the title, max 100 chars>",
   "blurb": "<one or two factual sentences on why it matters to builders, max 280 chars, plain text>"}
]

Order by importance, most important first. Write in English. Do not quote article text verbatim.`

// buildSystemPrompt returns the system prompt with the per-source cap
// interpolated. When maxPerSource <= 0 the cap block is stripped so the
// prompt matches the pre-cap behavior.
func buildSystemPrompt(maxPerSource int) string {
	if maxPerSource <= 0 {
		// Strip the DIVERSITY paragraph (and the blank line before it).
		const marker = "\n\nDIVERSITY"
		i := strings.Index(systemPromptTemplate, marker)
		if i < 0 {
			return systemPromptTemplate
		}
		j := strings.Index(systemPromptTemplate[i+2:], "\n\n")
		if j < 0 {
			return systemPromptTemplate[:i]
		}
		return systemPromptTemplate[:i] + systemPromptTemplate[i+2+j:]
	}
	return fmt.Sprintf(systemPromptTemplate, maxPerSource, maxPerSource)
}

// anthropicRequest mirrors /v1/messages. Only the fields we actually set.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []anthropicContent `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicResponse is the subset we need.
type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Error      *anthropicError    `json:"error,omitempty"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Summarize sends the articles to Claude and returns ranked summaries.
// The response is expected to be a JSON array in the first text block.
func (c *AnthropicClient) Summarize(ctx context.Context, req SummarizeRequest) ([]Summary, error) {
	if len(req.Articles) == 0 {
		return nil, nil
	}
	model := strs.FirstNonEmpty(req.Model, c.model)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	userText, err := buildUserPrompt(req.Articles, req.MaxPerSource)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	reqBody, err := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    []anthropicContent{{Type: "text", Text: buildSystemPrompt(req.MaxPerSource)}},
		Messages:  []anthropicMessage{{Role: "user", Content: userText}},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.http.Do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("call /v1/messages: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("call /v1/messages: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp anthropicResponse
	if err := json.Unmarshal(buf, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, snippet(buf))
	}
	if resp.StatusCode != http.StatusOK {
		if apiResp.Error != nil {
			return nil, fmt.Errorf("anthropic %s: %s", apiResp.Error.Type, apiResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic http %d: %s", resp.StatusCode, snippet(buf))
	}

	text := firstTextBlock(apiResp.Content)
	if text == "" {
		return nil, fmt.Errorf("no text block in response")
	}
	return parseSummaries(text)
}

func buildUserPrompt(articles []Article, maxPerSource int) (string, error) {
	type candidate struct {
		Index     int    `json:"index"`
		Source    string `json:"source"`
		Title     string `json:"title"`
		URL       string `json:"url"`
		Published string `json:"published,omitempty"`
		Summary   string `json:"summary,omitempty"`
	}
	cands := make([]candidate, len(articles))
	for i, a := range articles {
		cands[i] = candidate{
			Index:     i,
			Source:    a.Source,
			Title:     a.Title,
			URL:       a.URL,
			Published: a.Published,
			Summary:   truncateRunes(stripHTML(a.Body), 800),
		}
	}
	b, err := json.Marshal(cands)
	if err != nil {
		return "", err
	}
	header := "Candidates (pick 5 to 8):"
	if maxPerSource > 0 {
		header = fmt.Sprintf("Candidates (pick 5 to 8, max %d per source):", maxPerSource)
	}
	return header + "\n\n" + string(b), nil
}

func parseSummaries(s string) ([]Summary, error) {
	trimmed := extractJSONArray(s)
	if trimmed == "" {
		return nil, fmt.Errorf("no JSON array found in model output: %q", snippet([]byte(s)))
	}
	type raw struct {
		SourceIndex int    `json:"source_index"`
		Headline    string `json:"headline"`
		Blurb       string `json:"blurb"`
	}
	var rs []raw
	if err := json.Unmarshal([]byte(trimmed), &rs); err != nil {
		return nil, fmt.Errorf("decode summaries JSON: %w (raw: %s)", err, snippet([]byte(trimmed)))
	}
	out := make([]Summary, len(rs))
	for i, r := range rs {
		out[i] = Summary(r)
	}
	return out, nil
}

// extractJSONArray returns the substring between the first '[' and the
// matching closing ']'. It tolerates surrounding prose, code fences, or
// leading whitespace — which Claude sometimes emits despite instructions.
func extractJSONArray(s string) string {
	i := strings.IndexByte(s, '[')
	if i < 0 {
		return ""
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[i : j+1]
			}
		}
	}
	return ""
}

func firstTextBlock(blocks []anthropicContent) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

func truncateRunes(s string, maxRunes int) string {
	if len(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// stripHTML returns the plain-text content of an HTML fragment. Feed
// Summary/Content fields often arrive as HTML; we want readable input for
// the model without tags or entity references. Uses golang.org/x/net/html
// (already a transitive dep via gofeed) so that malformed markup — stray
// "<"/">", comments that could bridge tag boundaries, CDATA, entity refs —
// doesn't leak tag soup or smuggled text into the LLM prompt.
func stripHTML(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return strings.Join(strings.Fields(s), " ")
	}
	var b strings.Builder
	b.Grow(len(s))
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(b.String()), " ")
}

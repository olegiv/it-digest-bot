package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/strs"
)

// maxResponseBody caps the bytes read from Anthropic's /v1/messages
// response. Defense-in-depth against a misbehaving upstream; 4 MiB is
// well above the largest plausible response (max_tokens=1024 produces
// ~6 KB JSON in practice).
const maxResponseBody = 4 << 20

const sonnet55Model = "claude-sonnet-5-5"

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

Return your selection by calling the submit_summaries tool exactly once. Each summary entry has:
- source_index: the integer "index" of the chosen article
- headline: a concise rewrite of the title, max 100 chars
- blurb: one or two factual sentences on why it matters to builders, max 280 chars, plain text

Order summaries by importance, most important first. Write in English. Do not quote article text verbatim.`

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
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	System       []anthropicContent     `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	Tools        []anthropicTool        `json:"tools,omitempty"`
	ToolChoice   *anthropicToolChoice   `json:"tool_choice,omitempty"`
	Thinking     *anthropicThinking     `json:"thinking,omitempty"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

type anthropicThinking struct {
	Type string `json:"type"`
}

type anthropicOutputConfig struct {
	Effort string                `json:"effort"`
	Format anthropicOutputFormat `json:"format"`
}

type anthropicOutputFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicContent is one block in a request system prompt or response
// content array. Response blocks may be type "text" or "tool_use"; the
// extra fields are populated only on tool_use.
type anthropicContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// anthropicResponse is the subset we need.
type anthropicResponse struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Error      *anthropicError    `json:"error,omitempty"`
	Usage      *anthropicUsage    `json:"usage,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// submitSummariesToolName is the forced-call tool that returns the
// ranked summaries. Sonnet 4.6 rejects assistant-prefill, so structured
// output goes through tool_use instead.
const submitSummariesToolName = "submit_summaries"

// submitSummariesSchema is the JSON Schema for the tool's input. The
// schema is shared by legacy tool calls and Sonnet 5.5 JSON outputs.
const submitSummariesSchema = `{
  "type": "object",
  "properties": {
    "summaries": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "source_index": {"type": "integer", "description": "the \"index\" of the chosen candidate"},
          "headline":     {"type": "string",  "description": "concise title rewrite, max 100 chars"},
          "blurb":        {"type": "string",  "description": "1-2 sentences on why it matters, max 280 chars, plain text"}
        },
        "required": ["source_index", "headline", "blurb"],
        "additionalProperties": false
      }
    }
  },
  "required": ["summaries"],
  "additionalProperties": false
}`

// Summarize sends the articles to Claude and returns ranked summaries.
// Sonnet 5.5 returns schema-constrained JSON text; legacy models use a tool.
func (c *AnthropicClient) Summarize(ctx context.Context, req SummarizeRequest) ([]Summary, error) {
	if len(req.Articles) == 0 {
		return nil, nil
	}
	model := strs.FirstNonEmpty(req.Model, c.model)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
		if model == sonnet55Model {
			maxTokens = 2048
		}
	}

	userText, err := buildUserPrompt(req.Articles, req.MaxPerSource)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	// Force the model to call submit_summaries. tool_choice with a named
	// tool makes the API reject any response that does not invoke that
	// tool, which gives us guaranteed-shape JSON in tool_use.input —
	// no fragile text parsing, no chain-of-thought preambles eating
	// max_tokens, and (unlike assistant-prefill) supported on Sonnet 4.6.
	payload := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    []anthropicContent{{Type: "text", Text: buildSystemPrompt(req.MaxPerSource)}},
		Messages:  []anthropicMessage{{Role: "user", Content: userText}},
		Tools: []anthropicTool{{
			Name:        submitSummariesToolName,
			Description: "Submit the final ranked, summarized digest entries for posting to the channel.",
			InputSchema: json.RawMessage(submitSummariesSchema),
		}},
		ToolChoice: &anthropicToolChoice{Type: "tool", Name: submitSummariesToolName},
	}
	if model == sonnet55Model {
		payload.Tools = nil
		payload.ToolChoice = nil
		payload.Thinking = &anthropicThinking{Type: "between_tools"}
		payload.OutputConfig = &anthropicOutputConfig{
			Effort: "medium",
			Format: anthropicOutputFormat{Type: "json_schema", Schema: json.RawMessage(submitSummariesSchema)},
		}
		payload.System[0].Text = strings.Replace(payload.System[0].Text,
			"Return your selection by calling the submit_summaries tool exactly once.",
			`Return your selection as a JSON object with a "summaries" array matching the supplied schema. For every entry, copy source_index from that candidate's explicit "index" field, not its position in the array or an index inferred from another article. Ground the headline and blurb only in that same candidate's title and summary. Do not mix facts from different candidates or add facts from memory. Before returning, verify that each source_index points to the candidate actually summarized.`, 1)
	}
	reqBody, err := json.Marshal(payload)
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

	started := time.Now()
	defer func() {
		slog.InfoContext(ctx, "Anthropic request complete", "model", model, "elapsed", time.Since(started))
	}()
	resp, err := c.http.Do(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("call /v1/messages: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("call /v1/messages: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(buf)) > maxResponseBody {
		return nil, fmt.Errorf("anthropic response exceeds %d bytes", maxResponseBody)
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
	if apiResp.Usage != nil {
		slog.InfoContext(ctx, "Anthropic token usage", "model", model,
			"input_tokens", apiResp.Usage.InputTokens, "output_tokens", apiResp.Usage.OutputTokens)
	}
	if model == sonnet55Model {
		return decodeStructuredSummaries(apiResp, len(req.Articles))
	}

	tu := firstToolUse(apiResp.Content, submitSummariesToolName)
	if tu == nil {
		if apiResp.StopReason == "max_tokens" {
			return nil, fmt.Errorf("anthropic truncated at max_tokens before tool_use (raise llm.max_tokens or reduce candidates) (text=%q)", snippet([]byte(firstTextBlock(apiResp.Content))))
		}
		return nil, fmt.Errorf("no %s tool_use in response (stop_reason=%q, text=%q)",
			submitSummariesToolName, apiResp.StopReason, snippet([]byte(firstTextBlock(apiResp.Content))))
	}
	var input struct {
		Summaries []struct {
			SourceIndex int    `json:"source_index"`
			Headline    string `json:"headline"`
			Blurb       string `json:"blurb"`
		} `json:"summaries"`
	}
	if err := json.Unmarshal(tu.Input, &input); err != nil {
		return nil, fmt.Errorf("decode %s tool input: %w (raw: %s)",
			submitSummariesToolName, err, snippet(tu.Input))
	}
	out := make([]Summary, len(input.Summaries))
	for i, s := range input.Summaries {
		out[i] = Summary{SourceIndex: s.SourceIndex, Headline: s.Headline, Blurb: s.Blurb}
	}
	return out, nil
}

func decodeStructuredSummaries(resp anthropicResponse, articleCount int) ([]Summary, error) {
	switch resp.StopReason {
	case "refusal":
		return nil, fmt.Errorf("anthropic refused structured summaries (stop_reason=%q)", resp.StopReason)
	case "max_tokens":
		return nil, fmt.Errorf("anthropic truncated at max_tokens (raise llm.max_tokens or reduce candidates)")
	case "end_turn":
	default:
		return nil, fmt.Errorf("unexpected structured output stop_reason=%q", resp.StopReason)
	}
	text := firstTextBlock(resp.Content)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("missing structured summaries text (stop_reason=%q)", resp.StopReason)
	}
	var input struct {
		Summaries *[]struct {
			SourceIndex *int    `json:"source_index"`
			Headline    *string `json:"headline"`
			Blurb       *string `json:"blurb"`
		} `json:"summaries"`
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("decode structured summaries: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("structured summaries contain trailing content")
	}
	if input.Summaries == nil {
		return nil, fmt.Errorf("structured output missing summaries array")
	}
	out := make([]Summary, len(*input.Summaries))
	for i, s := range *input.Summaries {
		if s.SourceIndex == nil || s.Headline == nil || s.Blurb == nil {
			return nil, fmt.Errorf("structured summary %d missing required fields", i)
		}
		if *s.SourceIndex < 0 || *s.SourceIndex >= articleCount {
			return nil, fmt.Errorf("structured summary %d source_index %d outside candidate range", i, *s.SourceIndex)
		}
		out[i] = Summary{SourceIndex: *s.SourceIndex, Headline: *s.Headline, Blurb: *s.Blurb}
	}
	return out, nil
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

func firstTextBlock(blocks []anthropicContent) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

func firstToolUse(blocks []anthropicContent, name string) *anthropicContent {
	for i := range blocks {
		if blocks[i].Type == "tool_use" && blocks[i].Name == name {
			return &blocks[i]
		}
	}
	return nil
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

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func testHTTP() *httpx.Client {
	return httpx.New(httpx.WithSleep(nopSleep), httpx.WithMaxRetries(0))
}

func TestSummarizeHappyPath(t *testing.T) {
	t.Parallel()
	var gotAuth, gotVersion string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)

		fmt.Fprint(w, `{
          "content": [
            {"type":"tool_use","id":"toolu_1","name":"submit_summaries","input":{"summaries":[
              {"source_index":0,"headline":"H1","blurb":"B1"},
              {"source_index":2,"headline":"H2","blurb":"B2"}
            ]}}
          ],
          "stop_reason": "tool_use"
        }`)
	}))
	defer srv.Close()

	c := NewAnthropic("test-key", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	out, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{
			{Source: "OpenAI", Title: "A", URL: "https://a"},
			{Source: "Anthropic", Title: "B", URL: "https://b"},
			{Source: "DeepMind", Title: "C", URL: "https://c"},
		},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d summaries, want 2", len(out))
	}
	if out[0].Headline != "H1" || out[1].Headline != "H2" {
		t.Errorf("headlines = %+v", out)
	}
	if out[0].SourceIndex != 0 || out[1].SourceIndex != 2 {
		t.Errorf("indices = %+v", out)
	}
	if gotAuth != "test-key" {
		t.Errorf("x-api-key = %q", gotAuth)
	}
	if gotVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q", gotVersion)
	}
	if gotBody["model"] != "claude-sonnet-4-6" {
		t.Errorf("model = %v", gotBody["model"])
	}
}

func TestSummarize_RequestForcesToolChoice(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		fmt.Fprint(w, `{"content":[{"type":"tool_use","id":"t","name":"submit_summaries","input":{"summaries":[]}}],"stop_reason":"tool_use"}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	if _, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	}); err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	// tool_choice forces the tool the model must call.
	tc, ok := gotBody["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice = %#v, want map", gotBody["tool_choice"])
	}
	if tc["type"] != "tool" {
		t.Errorf("tool_choice.type = %v, want tool", tc["type"])
	}
	if tc["name"] != "submit_summaries" {
		t.Errorf("tool_choice.name = %v, want submit_summaries", tc["name"])
	}

	// tools[] declares the schema.
	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want 1 entry", gotBody["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "submit_summaries" {
		t.Errorf("tools[0].name = %v", tool["name"])
	}
	schema, ok := tool["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("tools[0].input_schema = %#v", tool["input_schema"])
	}
	if schema["type"] != "object" {
		t.Errorf("schema.type = %v, want object", schema["type"])
	}

	// Conversation must end with a single user message — no assistant prefill.
	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %#v, want 1 entry", gotBody["messages"])
	}
	if m := msgs[0].(map[string]any); m["role"] != "user" {
		t.Errorf("messages[0].role = %v, want user", m["role"])
	}
}

func TestSummarize_MaxTokensTruncationError(t *testing.T) {
	t.Parallel()
	// Model ran out of tokens before emitting tool_use — content has only
	// a partial text block and stop_reason is max_tokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"Looking at the candidates,"}],"stop_reason":"max_tokens"}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	_, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "truncated at max_tokens") {
		t.Errorf("error %q does not mention max_tokens truncation", err.Error())
	}
}

func TestSummarize_NoToolUseError(t *testing.T) {
	t.Parallel()
	// Model returned only text, no tool_use block — should not happen with
	// forced tool_choice, but we surface stop_reason for diagnosis.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"i refuse"}],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	_, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `stop_reason="end_turn"`) {
		t.Errorf("error %q missing stop_reason", err.Error())
	}
	if !strings.Contains(err.Error(), "submit_summaries") {
		t.Errorf("error %q missing tool name for diagnosis", err.Error())
	}
}

func TestSummarize_MalformedToolInput(t *testing.T) {
	t.Parallel()
	// tool_use.input has wrong field type (source_index as string).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"tool_use","id":"t","name":"submit_summaries","input":{"summaries":[{"source_index":"zero","headline":"H","blurb":"B"}]}}],"stop_reason":"tool_use"}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	_, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode submit_summaries tool input") {
		t.Errorf("error %q does not flag the decode failure", err.Error())
	}
}

func TestSummarizeAPIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	_, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	})
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Errorf("expected bad input error, got %v", err)
	}
}

func TestSummarizeEmptyInput(t *testing.T) {
	t.Parallel()
	c := NewAnthropic("k", "m", testHTTP())
	out, err := c.Summarize(context.Background(), SummarizeRequest{})
	if err != nil {
		t.Errorf("Summarize with empty articles: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output for empty input, got %+v", out)
	}
}

func TestStripHTML(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "basic tags and whitespace",
			in:   `<p>hello <a href="x">world</a></p> and   more`,
			want: "hello world and more",
		},
		{
			name: "decodes entity references",
			in:   `use <code>x &lt; y</code> here`,
			want: "use x < y here",
		},
		{
			name: "drops script content",
			in:   `before<script>alert("INJECTED")</script>after`,
			want: "beforeafter",
		},
		{
			name: "drops style content",
			in:   `before<style>body{x:1}</style>after`,
			want: "beforeafter",
		},
		{
			name: "comment cannot bridge text outside its bounds",
			in:   `visible <!-- hidden --> tail`,
			want: "visible tail",
		},
		{
			name: "unescaped angle brackets in prose survive as text",
			in:   `if x < 5 then y > 3`,
			want: "if x < 5 then y > 3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripHTML(tc.in); got != tc.want {
				t.Errorf("in=%q: got %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

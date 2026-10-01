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

func TestSummarizeSonnet55Request(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		clientModel string
		reqModel    string
		maxTokens   int
		wantTokens  int
	}{
		{name: "model default", clientModel: sonnet55Model, wantTokens: 2048},
		{name: "request model override", clientModel: "claude-sonnet-4-6", reqModel: sonnet55Model, wantTokens: 2048},
		{name: "explicit budget", clientModel: sonnet55Model, maxTokens: 3072, wantTokens: 3072},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"content":[{"type":"thinking","text":"ignore this"},{"type":"text","text":"{\"summaries\":[{\"source_index\":0,\"headline\":\"H\",\"blurb\":\"B\"}]}"}],"stop_reason":"end_turn","usage":{"input_tokens":123,"output_tokens":45}}`)
			}))
			defer srv.Close()
			client := NewAnthropic("test-key", tc.clientModel, testHTTP(), WithAnthropicBaseURL(srv.URL))
			out, err := client.Summarize(context.Background(), SummarizeRequest{
				Model: tc.reqModel, MaxTokens: tc.maxTokens, MaxPerSource: 2,
				Articles: []Article{{Source: "S", Title: "T", URL: "https://example.com"}},
			})
			if err != nil || len(out) != 1 || out[0].SourceIndex != 0 || out[0].Headline != "H" || out[0].Blurb != "B" {
				t.Fatalf("out = %+v, err = %v", out, err)
			}
			if body["model"] != sonnet55Model || body["max_tokens"] != float64(tc.wantTokens) {
				t.Fatalf("model/budget = %v/%v", body["model"], body["max_tokens"])
			}
			for _, key := range []string{"tools", "tool_choice"} {
				if _, ok := body[key]; ok {
					t.Errorf("unexpected %s", key)
				}
			}
			thinking := body["thinking"].(map[string]any)
			if thinking["type"] != "between_tools" {
				t.Errorf("thinking = %v", thinking)
			}
			output := body["output_config"].(map[string]any)
			format := output["format"].(map[string]any)
			if output["effort"] != "medium" || format["type"] != "json_schema" {
				t.Errorf("output_config = %v", output)
			}
			schema := format["schema"].(map[string]any)
			properties := schema["properties"].(map[string]any)
			items := properties["summaries"].(map[string]any)["items"].(map[string]any)
			if schema["additionalProperties"] != false || items["additionalProperties"] != false {
				t.Errorf("schema permits extra properties: %v", schema)
			}
			if len(schema["required"].([]any)) != 1 || len(items["required"].([]any)) != 3 {
				t.Errorf("schema missing required fields: %v", schema)
			}
			prompt := body["system"].([]any)[0].(map[string]any)["text"].(string)
			if strings.Contains(prompt, "submit_summaries") || !strings.Contains(prompt, "JSON object") || !strings.Contains(prompt, "DIVERSITY") {
				t.Errorf("unexpected prompt: %s", prompt)
			}
		})
	}
}

func TestSummarizeSonnet55Responses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		text       string
		stopReason string
		wantErr    string
	}{
		{name: "empty summaries", text: `{"summaries":[]}`, stopReason: "end_turn"},
		{name: "missing text", stopReason: "end_turn", wantErr: "missing structured"},
		{name: "malformed JSON", text: `{`, stopReason: "end_turn", wantErr: "decode structured"},
		{name: "prose before JSON", text: `Here you go: {"summaries":[]}`, stopReason: "end_turn", wantErr: "decode structured"},
		{name: "trailing prose", text: `{"summaries":[]} done`, stopReason: "end_turn", wantErr: "trailing content"},
		{name: "missing array", text: `{}`, stopReason: "end_turn", wantErr: "missing summaries"},
		{name: "null array", text: `{"summaries":null}`, stopReason: "end_turn", wantErr: "missing summaries"},
		{name: "missing index", text: `{"summaries":[{"headline":"H","blurb":"B"}]}`, stopReason: "end_turn", wantErr: "missing required"},
		{name: "invalid index", text: `{"summaries":[{"source_index":1,"headline":"H","blurb":"B"}]}`, stopReason: "end_turn", wantErr: "outside candidate range"},
		{name: "wrong type", text: `{"summaries":[{"source_index":"zero","headline":"H","blurb":"B"}]}`, stopReason: "end_turn", wantErr: "decode structured"},
		{name: "unknown field", text: `{"summaries":[],"extra":true}`, stopReason: "end_turn", wantErr: "decode structured"},
		{name: "refusal", text: "Cannot comply", stopReason: "refusal", wantErr: "refused"},
		{name: "truncation with valid JSON", text: `{"summaries":[]}`, stopReason: "max_tokens", wantErr: "truncated at max_tokens"},
		{name: "unexpected stop", text: `{"summaries":[]}`, stopReason: "tool_use", wantErr: "unexpected structured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				response := anthropicResponse{
					Content: []anthropicContent{{Type: "text", Text: tc.text}}, StopReason: tc.stopReason,
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer srv.Close()
			client := NewAnthropic("k", sonnet55Model, testHTTP(), WithAnthropicBaseURL(srv.URL))
			out, err := client.Summarize(context.Background(), SummarizeRequest{Articles: []Article{{Source: "S", Title: "T", URL: "https://example.com"}}})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
			} else if err != nil || len(out) != 0 {
				t.Fatalf("want empty summaries, got %+v, %v", out, err)
			}
		})
	}
}

func TestSummarizeSonnet55LegacyOverride(t *testing.T) {
	t.Parallel()
	var body anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"content":[{"type":"tool_use","name":"submit_summaries","input":{"summaries":[]}}],"stop_reason":"tool_use"}`)
	}))
	defer srv.Close()
	client := NewAnthropic("k", sonnet55Model, testHTTP(), WithAnthropicBaseURL(srv.URL))
	_, err := client.Summarize(context.Background(), SummarizeRequest{
		Model: "claude-sonnet-4-6", Articles: []Article{{Title: "T"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body.MaxTokens != 1024 || body.Thinking != nil || body.OutputConfig != nil || body.ToolChoice == nil || body.ToolChoice.Type != "tool" {
		t.Fatalf("legacy request = %+v", body)
	}
}

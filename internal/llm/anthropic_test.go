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
            {"type":"text","text":"[{\"source_index\":0,\"headline\":\"H1\",\"blurb\":\"B1\"},{\"source_index\":2,\"headline\":\"H2\",\"blurb\":\"B2\"}]"}
          ],
          "stop_reason": "end_turn"
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

func TestSummarizeHandlesProseAroundJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"Sure, here is the list:\n\n[{\"source_index\":0,\"headline\":\"Only one\",\"blurb\":\"x\"}]\n\nLet me know if you want more."}]}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	out, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 1 || out[0].Headline != "Only one" {
		t.Errorf("out = %+v", out)
	}
}

func TestSummarize_PrefillsAssistantBracket(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		fmt.Fprint(w, `{"content":[{"type":"text","text":"{\"source_index\":0,\"headline\":\"H\",\"blurb\":\"B\"}]"}],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()

	c := NewAnthropic("k", "claude-sonnet-4-6", testHTTP(), WithAnthropicBaseURL(srv.URL))
	out, err := c.Summarize(context.Background(), SummarizeRequest{
		Articles: []Article{{Source: "s", Title: "t", URL: "u"}},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(out) != 1 || out[0].Headline != "H" {
		t.Fatalf("out = %+v", out)
	}

	msgs, ok := gotBody["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages = %#v, want 2 entries", gotBody["messages"])
	}
	prefill, ok := msgs[1].(map[string]any)
	if !ok {
		t.Fatalf("messages[1] = %#v, want map", msgs[1])
	}
	if prefill["role"] != "assistant" {
		t.Errorf("messages[1].role = %v, want assistant", prefill["role"])
	}
	if prefill["content"] != "[" {
		t.Errorf("messages[1].content = %q, want %q", prefill["content"], "[")
	}
}

func TestSummarize_MaxTokensTruncationError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"Looking at the candidates, I need to pick the most"}],"stop_reason":"max_tokens"}`)
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

func TestSummarize_ParseErrorIncludesStopReason(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"content":[{"type":"text","text":"not json at all"}],"stop_reason":"end_turn"}`)
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
		t.Errorf("error %q does not include stop_reason", err.Error())
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

func TestExtractJSONArray(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{`[1,2,3]`, `[1,2,3]`},
		{`prose [1,2] trailing`, `[1,2]`},
		{"```\n[{\"a\":1}]\n```", `[{"a":1}]`},
		{`nested [[1], [2,3]] end`, `[[1], [2,3]]`},
		{`no array`, ``},
		{`[unclosed`, ``},
	}
	for _, tc := range cases {
		got := extractJSONArray(tc.in)
		if got != tc.want {
			t.Errorf("in=%q: got %q, want %q", tc.in, got, tc.want)
		}
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

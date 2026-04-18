package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

func nopSleep(_ context.Context, _ time.Duration) error { return nil }

func TestSendMessageSuccess(t *testing.T) {
	t.Parallel()
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":4242}}`))
	}))
	defer srv.Close()

	bot := New("test-token",
		WithBaseURL(srv.URL),
		WithHTTPClient(httpx.New(httpx.WithSleep(nopSleep))),
	)
	id, err := bot.SendMessage(context.Background(), "@channel", "hello", ParseModeMarkdownV2)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != 4242 {
		t.Errorf("id = %d", id)
	}
	if gotBody["chat_id"] != "@channel" {
		t.Errorf("chat_id = %v", gotBody["chat_id"])
	}
	if gotBody["text"] != "hello" {
		t.Errorf("text = %v", gotBody["text"])
	}
	if gotBody["parse_mode"] != "MarkdownV2" {
		t.Errorf("parse_mode = %v", gotBody["parse_mode"])
	}
}

func TestSendMessageAPIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()

	bot := New("t",
		WithBaseURL(srv.URL),
		WithHTTPClient(httpx.New(httpx.WithSleep(nopSleep))),
	)
	_, err := bot.SendMessage(context.Background(), "@nope", "hi", ParseModeNone)
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("expected 'chat not found' error, got %v", err)
	}
}

func TestSendMessageTooLong(t *testing.T) {
	t.Parallel()
	bot := New("t")
	huge := strings.Repeat("x", MaxMessageBytes+1)
	_, err := bot.SendMessage(context.Background(), "@c", huge, ParseModeNone)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("expected 'too long' error, got %v", err)
	}
}

func TestSanitizeURLMasksBotToken(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{"https://api.telegram.org/bot1234567:AABBCC/sendMessage", "https://api.telegram.org/bot-REDACTED/sendMessage"},
		{"https://api.telegram.org/bot1234567:AABBCC/getMe", "https://api.telegram.org/bot-REDACTED/getMe"},
		{"https://example.com/path?x=1", "https://example.com/path?x=1"},
		{"https://example.com/botsomething-but-no-token", "https://example.com/bot-REDACTED"},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.raw, err)
		}
		if got := SanitizeURL(u); got != tc.want {
			t.Errorf("SanitizeURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestSanitizeURLNilSafe(t *testing.T) {
	t.Parallel()
	if got := SanitizeURL(nil); got != "" {
		t.Errorf("SanitizeURL(nil) = %q, want empty", got)
	}
}

func TestSendMessageErrorRedactsTokenInPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	const token = "1234567:SECRET-TOKEN-DO-NOT-LOG"
	// Inject a BARE httpx.Client — no explicit sanitizer. telegram.New must
	// install SanitizeURL defensively; this regression-covers the latent
	// SECRETS-001 bug where watch.go passed a shared client and lost the
	// sanitizer that telegram.New's default would have provided.
	bareClient := httpx.New(
		httpx.WithSleep(nopSleep),
		httpx.WithMaxRetries(1),
	)
	bot := New(token,
		WithBaseURL(srv.URL),
		WithHTTPClient(bareClient),
	)
	_, err := bot.SendMessage(context.Background(), "@c", "hi", ParseModeNone)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error leaked bot token: %v", err)
	}
	if !strings.Contains(err.Error(), "/bot-REDACTED/") {
		t.Errorf("expected masked /bot-REDACTED/ segment in error, got %v", err)
	}
}

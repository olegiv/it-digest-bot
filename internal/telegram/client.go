// Package telegram is a minimal Bot API client for the two operations this
// service needs: sending messages and escaping user content into MarkdownV2.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/olegiv/it-digest-bot/internal/httpx"
)

// MaxMessageBytes is Telegram's hard cap on sendMessage text length
// (4096 UTF-16 code units in theory; 4096 bytes is a conservative floor
// for multi-byte content).
const MaxMessageBytes = 4096

// maxResponseBody caps the bytes read from a /sendMessage response.
// Defense-in-depth — Bot API responses are typically <1 KB.
const maxResponseBody = 1 << 20

// ParseMode is the Telegram Bot API "parse_mode" parameter.
type ParseMode string

const (
	ParseModeNone       ParseMode = ""
	ParseModeMarkdownV2 ParseMode = "MarkdownV2"
	ParseModeHTML       ParseMode = "HTML"
)

// Bot is a small wrapper around the Bot API's /sendMessage endpoint.
type Bot struct {
	token   string
	baseURL string
	http    *httpx.Client
}

// Option configures a Bot.
type Option func(*Bot)

// WithBaseURL overrides the Bot API base (useful for tests).
func WithBaseURL(u string) Option {
	return func(b *Bot) { b.baseURL = u }
}

// WithHTTPClient injects a custom httpx.Client.
func WithHTTPClient(c *httpx.Client) Option {
	return func(b *Bot) { b.http = c }
}

// New returns a Bot bound to the given token. The bot token is embedded in
// the URL path (/bot<TOKEN>/…), so New unconditionally installs SanitizeURL
// on its httpx.Client — even if the caller injected a bare client via
// WithHTTPClient. This mutates the injected client; callers who share one
// httpx.Client across unrelated services should give Bot its own.
func New(token string, opts ...Option) *Bot {
	b := &Bot{
		token:   token,
		baseURL: "https://api.telegram.org",
		http:    httpx.New(),
	}
	for _, o := range opts {
		o(b)
	}
	b.http.SetURLSanitizer(SanitizeURL)
	return b
}

// botPathRE matches the "/bot<token>" segment of a Telegram API URL path.
// The token itself has no "/" and runs to the next path separator.
var botPathRE = regexp.MustCompile(`/bot[^/]+`)

// SanitizeURL masks the /bot<TOKEN>/ prefix of Telegram Bot API URLs so
// the bot token does not appear in error messages. Wired into the default
// httpx.Client via httpx.WithURLSanitizer; safe for non-Telegram URLs
// (which pass through url.URL.Redacted unchanged). The placeholder uses
// only unreserved characters to avoid URL re-escaping on String/Redacted.
func SanitizeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	cp := *u
	if cp.Path != "" {
		cp.Path = botPathRE.ReplaceAllString(cp.Path, "/bot-REDACTED")
	}
	if cp.RawPath != "" {
		cp.RawPath = botPathRE.ReplaceAllString(cp.RawPath, "/bot-REDACTED")
	}
	return cp.Redacted()
}

// SendMessage posts `text` to `chat` and returns the Telegram message_id on
// success. `chat` is a channel username (e.g. "@my_channel") or a numeric
// chat id as a string.
func (b *Bot) SendMessage(ctx context.Context, chat, text string, mode ParseMode) (int64, error) {
	if len(text) > MaxMessageBytes {
		return 0, fmt.Errorf("message too long: %d bytes (max %d)", len(text), MaxMessageBytes)
	}
	payload := map[string]any{
		"chat_id": chat,
		"text":    text,
	}
	if mode != ParseModeNone {
		payload["parse_mode"] = string(mode)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal sendMessage: %w", err)
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", b.baseURL, b.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build sendMessage request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.http.Do(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("send to telegram: %w", err)
	}
	if resp == nil {
		return 0, fmt.Errorf("send to telegram: nil response")
	}
	defer func() { _ = resp.Body.Close() }()

	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return 0, fmt.Errorf("read telegram response: %w", err)
	}
	if int64(len(buf)) > maxResponseBody {
		return 0, fmt.Errorf("telegram response exceeds %d bytes", maxResponseBody)
	}

	var tgResp apiResponse[messageResult]
	if err := json.Unmarshal(buf, &tgResp); err != nil {
		return 0, fmt.Errorf("decode telegram response: %w (body: %s)",
			err, snippet(buf))
	}
	if !tgResp.OK {
		return 0, fmt.Errorf("telegram error: code=%d %s",
			tgResp.ErrorCode, tgResp.Description)
	}
	return tgResp.Result.MessageID, nil
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

type messageResult struct {
	MessageID int64 `json:"message_id"`
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return strings.ReplaceAll(s, "\n", " ")
}

// TokenFromEnv is a convenience for wiring up from a validated Config.
// Returns an error if the token is empty.
func TokenFromEnv(v string) (string, error) {
	if v == "" {
		return "", errors.New("empty telegram bot token")
	}
	return v, nil
}

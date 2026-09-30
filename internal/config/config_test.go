package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validTOML = `
[telegram]
channel = "@test_channel"

[database]
path = "/var/lib/it-digest/state.db"

[claudecode]
npm_package = "@anthropic-ai/claude-code"
github_repo = "anthropics/claude-code"

[llm]
model      = "claude-sonnet-4-6"
max_tokens = 1024

[log]
level  = "info"
format = "json"

[[feed]]
name = "OpenAI"
url  = "https://openai.com/blog/rss.xml"
`

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	t.Setenv(EnvAnthropicAPIKey, "anthropic-stub")
	p := writeConfig(t, validTOML)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Telegram.Channel != "@test_channel" {
		t.Errorf("channel = %q", cfg.Telegram.Channel)
	}
	if cfg.Telegram.BotToken != "tg-stub" {
		t.Errorf("bot token not overlaid: %q", cfg.Telegram.BotToken)
	}
	if cfg.LLM.APIKey != "anthropic-stub" {
		t.Errorf("api key not overlaid: %q", cfg.LLM.APIKey)
	}
	if len(cfg.Feeds) != 1 || cfg.Feeds[0].Name != "OpenAI" {
		t.Errorf("feeds = %+v", cfg.Feeds)
	}
	if cfg.ClaudeCode.GitHubToken != "" {
		t.Errorf("GitHubToken should default to empty when env unset: %q", cfg.ClaudeCode.GitHubToken)
	}
}

func TestLoadOverlaysGitHubToken(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	t.Setenv(EnvAnthropicAPIKey, "anthropic-stub")
	t.Setenv(EnvGitHubToken, "ghp_abc123")
	p := writeConfig(t, validTOML)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClaudeCode.GitHubToken != "ghp_abc123" {
		t.Errorf("GITHUB_TOKEN not overlaid: %q", cfg.ClaudeCode.GitHubToken)
	}
}

func TestLoadMissingTelegramToken(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "")
	p := writeConfig(t, validTOML)

	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), EnvTelegramBotToken) {
		t.Fatalf("expected telegram token error, got %v", err)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	bad := validTOML + "\n[unexpected]\nfoo = 1\n"
	p := writeConfig(t, bad)

	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "unknown config keys") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

func TestValidateForDaily(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	t.Setenv(EnvAnthropicAPIKey, "")
	p := writeConfig(t, validTOML)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.ValidateForDaily(); err == nil ||
		!strings.Contains(err.Error(), EnvAnthropicAPIKey) {
		t.Errorf("expected anthropic-key error, got %v", err)
	}

	cfg.LLM.APIKey = "anthropic-stub"
	if err := cfg.ValidateForDaily(); err != nil {
		t.Errorf("unexpected error after setting API key: %v", err)
	}
}

func TestValidateRejectsBadLogLevel(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	bad := strings.Replace(validTOML, `level  = "info"`, `level  = "trace"`, 1)
	p := writeConfig(t, bad)

	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "log.level") {
		t.Fatalf("expected log.level error, got %v", err)
	}
}

func TestValidateForDailyRequiresFeeds(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	t.Setenv(EnvAnthropicAPIKey, "anthropic-stub")
	noFeeds := strings.Split(validTOML, "[[feed]]")[0]
	p := writeConfig(t, noFeeds)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.ValidateForDaily(); err == nil ||
		!strings.Contains(err.Error(), "feed") {
		t.Errorf("expected feed-required error, got %v", err)
	}
}

func TestLoadDrupalSecurityFeedURL(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")

	tests := []struct {
		name    string
		feedURL string
		wantErr string
	}{
		{name: "section absent", feedURL: "", wantErr: ""},
		{name: "https override", feedURL: "https://mirror.example.org/security/all/rss.xml", wantErr: ""},
		{name: "loopback http for fixtures", feedURL: "http://127.0.0.1:8000/feed.xml", wantErr: ""},
		{name: "localhost http for fixtures", feedURL: "http://localhost:8000/feed.xml", wantErr: ""},
		{name: "upper-case localhost", feedURL: "http://LOCALHOST:8000/feed.xml", wantErr: ""},
		{name: "ipv6 loopback", feedURL: "http://[::1]:8000/feed.xml", wantErr: ""},
		{name: "non-loopback ip rejected", feedURL: "http://10.0.0.1/feed.xml", wantErr: "drupal_security.feed_url must use https"},
		{name: "unparsable rejected", feedURL: "https://%zz/feed.xml", wantErr: "drupal_security.feed_url is not a valid URL"},
		{name: "remote http rejected", feedURL: "http://example.org/feed.xml", wantErr: "drupal_security.feed_url must use https"},
		{name: "file scheme rejected", feedURL: "file:///etc/passwd", wantErr: "drupal_security.feed_url must use https"},
		{name: "missing host rejected", feedURL: "https:///feed.xml", wantErr: "drupal_security.feed_url must include a host"},
		{name: "credentials rejected", feedURL: "https://user:secret@mirror.example.org/feed.xml", wantErr: "drupal_security.feed_url must not contain credentials"},
		{name: "query string rejected", feedURL: "https://mirror.example.org/feed.xml?token=abc", wantErr: "drupal_security.feed_url must not contain a query string or fragment"},
		{name: "fragment rejected", feedURL: "https://mirror.example.org/feed.xml#frag", wantErr: "drupal_security.feed_url must not contain a query string or fragment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := validTOML
			if tt.feedURL != "" {
				content += "\n[drupal_security]\nfeed_url = \"" + tt.feedURL + "\"\n"
			}
			cfg, err := Load(writeConfig(t, content))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if cfg.DrupalSecurity.FeedURL != tt.feedURL {
					t.Errorf("feed_url = %q, want %q", cfg.DrupalSecurity.FeedURL, tt.feedURL)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadRejectsUnknownDrupalSecurityKey(t *testing.T) {
	t.Setenv(EnvTelegramBotToken, "tg-stub")
	p := writeConfig(t, validTOML+"\n[drupal_security]\nenabled = true\n")
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "unknown config keys") {
		t.Fatalf("expected unknown-key error, got %v", err)
	}
}

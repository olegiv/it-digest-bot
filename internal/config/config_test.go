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

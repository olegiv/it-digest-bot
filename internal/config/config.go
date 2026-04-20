// Package config loads and validates the service's TOML configuration and
// overlays secrets from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	EnvTelegramBotToken = "TELEGRAM_BOT_TOKEN"
	EnvAnthropicAPIKey  = "ANTHROPIC_API_KEY"
	EnvGitHubToken      = "GITHUB_TOKEN"
)

// Config is the full parsed configuration.
type Config struct {
	Telegram   TelegramConfig   `toml:"telegram"`
	Database   DatabaseConfig   `toml:"database"`
	ClaudeCode ClaudeCodeConfig `toml:"claudecode"`
	LLM        LLMConfig        `toml:"llm"`
	Log        LogConfig        `toml:"log"`
	Digest     DigestConfig     `toml:"digest"`
	Feeds      []FeedConfig     `toml:"feed"`
}

type TelegramConfig struct {
	Channel   string `toml:"channel"`
	AdminChat string `toml:"admin_chat"` // optional; falls back to Channel for OnFailure alerts
	BotToken  string `toml:"-"`          // from env only
}

type DatabaseConfig struct {
	Path string `toml:"path"`
}

type ClaudeCodeConfig struct {
	NPMPackage  string `toml:"npm_package"`
	GitHubRepo  string `toml:"github_repo"`
	GitHubToken string `toml:"-"` // optional, from GITHUB_TOKEN env
}

type LLMConfig struct {
	Model     string `toml:"model"`
	MaxTokens int    `toml:"max_tokens"`
	APIKey    string `toml:"-"` // from env only
}

type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

type DigestConfig struct {
	LookbackHours int `toml:"lookback_hours"`
	// MaxPerSource caps how many items from any single feed source can
	// appear in one digest. 0 means "use the default"; set explicitly to
	// a negative value to disable the cap entirely. Defense-in-depth
	// against an LLM that ignores the per-source instruction in the
	// system prompt.
	MaxPerSource int `toml:"max_per_source"`
}

// DefaultMaxPerSource is applied when DigestConfig.MaxPerSource is 0.
const DefaultMaxPerSource = 2

type FeedConfig struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

// Load reads TOML from path, overlays secrets from env, and validates.
// It returns an error if any required field is missing or malformed.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &Config{}
	meta, err := toml.Decode(string(b), cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if undec := meta.Undecoded(); len(undec) > 0 {
		keys := make([]string, 0, len(undec))
		for _, k := range undec {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("unknown config keys: %s", strings.Join(keys, ", "))
	}

	cfg.Telegram.BotToken = os.Getenv(EnvTelegramBotToken)
	cfg.LLM.APIKey = os.Getenv(EnvAnthropicAPIKey)
	cfg.ClaudeCode.GitHubToken = os.Getenv(EnvGitHubToken)

	if cfg.Digest.MaxPerSource == 0 {
		cfg.Digest.MaxPerSource = DefaultMaxPerSource
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate enforces phase-1 invariants. The LLM / API-key requirement is
// deferred to ValidateForDaily so that `digest watch` doesn't need the
// Anthropic key to run.
func (c *Config) Validate() error {
	var errs []string
	if c.Telegram.Channel == "" {
		errs = append(errs, "telegram.channel is required")
	}
	if c.Telegram.BotToken == "" {
		errs = append(errs, EnvTelegramBotToken+" env var is required")
	}
	if c.Database.Path == "" {
		errs = append(errs, "database.path is required")
	}
	if c.ClaudeCode.NPMPackage == "" {
		errs = append(errs, "claudecode.npm_package is required")
	}
	if c.ClaudeCode.GitHubRepo == "" {
		errs = append(errs, "claudecode.github_repo is required")
	}
	switch c.Log.Level {
	case "", "debug", "info", "warn", "error":
	default:
		errs = append(errs, "log.level must be one of: debug, info, warn, error")
	}
	switch c.Log.Format {
	case "", "json", "text":
	default:
		errs = append(errs, "log.format must be one of: json, text")
	}
	if c.Digest.LookbackHours < 0 {
		errs = append(errs, "digest.lookback_hours must be >= 0")
	}
	if len(errs) > 0 {
		return errors.New("invalid config: " + strings.Join(errs, "; "))
	}
	return nil
}

// ValidateForDaily adds phase-2-specific checks on top of Validate.
func (c *Config) ValidateForDaily() error {
	if err := c.Validate(); err != nil {
		return err
	}
	var errs []string
	if c.LLM.APIKey == "" {
		errs = append(errs, EnvAnthropicAPIKey+" env var is required for digest daily")
	}
	if c.LLM.Model == "" {
		errs = append(errs, "llm.model is required for digest daily")
	}
	if c.LLM.MaxTokens <= 0 {
		errs = append(errs, "llm.max_tokens must be > 0")
	}
	if len(c.Feeds) == 0 {
		errs = append(errs, "at least one [[feed]] entry is required for digest daily")
	}
	for i, f := range c.Feeds {
		if f.Name == "" {
			errs = append(errs, fmt.Sprintf("feed[%d].name is required", i))
		}
		if f.URL == "" {
			errs = append(errs, fmt.Sprintf("feed[%d].url is required", i))
		}
	}
	if len(errs) > 0 {
		return errors.New("invalid config for daily: " + strings.Join(errs, "; "))
	}
	return nil
}

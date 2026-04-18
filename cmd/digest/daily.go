package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/config"
	"github.com/olegiv/it-digest-bot/internal/digest"
	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/llm"
	"github.com/olegiv/it-digest-bot/internal/news"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newDailyCmd(flags *rootFlags) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "daily",
		Short: "Build and post the daily AI news digest",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, log, err := loadConfigAndLogger(flags.configPath, "daily")
			if err != nil {
				return err
			}
			if err := cfg.ValidateForDaily(); err != nil {
				if !dryRun || !isTelegramTokenOnly(err) {
					return err
				}
				log.Warn("dry-run: TELEGRAM_BOT_TOKEN missing — continuing without Telegram send", "err", err)
			}

			st, err := store.Open(ctx, "file:"+cfg.Database.Path)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer func() { _ = st.Close() }()
			if err := st.Migrate(ctx); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			// Per-API httpx clients. NewFeedFetcher and telegram.New defensively
			// install their own SanitizeURL on whatever client they get, so we
			// keep clients separate to avoid one constructor's sanitizer
			// overwriting another on a shared client.
			apiHTTP := httpx.New()
			feedHTTP := httpx.New()
			tgHTTP := httpx.New()

			var fetcherOpts []news.FeedFetcherOption
			if cfg.Digest.LookbackHours > 0 {
				fetcherOpts = append(fetcherOpts,
					news.WithLookback(time.Duration(cfg.Digest.LookbackHours)*time.Hour))
			}
			builder := &digest.Builder{
				Fetcher:    news.NewFeedFetcher(feedsFromConfig(cfg.Feeds), feedHTTP, fetcherOpts...),
				Summarizer: llm.NewAnthropic(cfg.LLM.APIKey, cfg.LLM.Model, apiHTTP),
				Bot:        telegram.New(cfg.Telegram.BotToken, telegram.WithHTTPClient(tgHTTP)),
				Channel:    cfg.Telegram.Channel,
				Model:      cfg.LLM.Model,
				MaxTokens:  cfg.LLM.MaxTokens,
				Articles:   st.Articles,
				Posts:      st.Posts,
				Logger:     log,
				DryRun:     dryRun,
				DryOut:     cmd.OutOrStdout(),
			}

			res, err := builder.Run(ctx)
			if err != nil {
				return fmt.Errorf("digest: %w", err)
			}
			log.Info("daily complete",
				"fetched", res.Fetched,
				"new_items", res.NewItems,
				"summaries", res.Summaries,
				"messages", res.Messages,
				"dry_run", dryRun)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"fetch + summarize + render, but print to stdout instead of posting to Telegram and skip DB writes")
	return cmd
}

func feedsFromConfig(cfgFeeds []config.FeedConfig) []news.Feed {
	out := make([]news.Feed, len(cfgFeeds))
	for i, f := range cfgFeeds {
		out[i] = news.Feed{Name: f.Name, URL: f.URL}
	}
	return out
}

// isTelegramTokenOnly returns true when the validation error is purely
// about the missing Telegram bot token — so dry-run can proceed.
func isTelegramTokenOnly(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, config.EnvTelegramBotToken) {
		return false
	}
	for _, other := range []string{
		"claudecode.", "database.", "llm.", "log.", "feed[",
		config.EnvAnthropicAPIKey,
	} {
		if strings.Contains(msg, other) {
			return false
		}
	}
	return true
}

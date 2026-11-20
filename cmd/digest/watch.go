package main

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/applerelease"
	"github.com/olegiv/it-digest-bot/internal/claudecode"
	"github.com/olegiv/it-digest-bot/internal/config"
	"github.com/olegiv/it-digest-bot/internal/drupalsec"
	"github.com/olegiv/it-digest-bot/internal/gorelease"
	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/phprelease"
	"github.com/olegiv/it-digest-bot/internal/phpsec"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newWatchCmd(flags *rootFlags) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Check Claude Code, Go, PHP, iOS/macOS releases plus Drupal and PHP security advisories and post them",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, log, err := loadConfigAndLogger(flags.configPath, "watch")
			if err != nil {
				return err
			}

			st, err := store.Open(ctx, "file:"+cfg.Database.Path)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer func() { _ = st.Close() }()
			if err := st.Migrate(ctx); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			// Keep Telegram on its own client because telegram.New installs a
			// defensive URL sanitizer on the injected client.
			apiHTTP := httpx.New()
			tgHTTP := httpx.New()

			sources := []releasewatch.Source{
				&claudecode.Source{
					Package:    cfg.ClaudeCode.NPMPackage,
					GitHubRepo: cfg.ClaudeCode.GitHubRepo,
					NPM:        claudecode.NewNPMClient(apiHTTP),
					GitHub:     claudecode.NewGitHubClient(apiHTTP, cfg.ClaudeCode.GitHubToken),
					Logger:     log,
				},
				gorelease.NewSource(apiHTTP),
				phpReleaseSource(cfg, apiHTTP, log),
				phpSecuritySource(cfg, apiHTTP, log),
				drupalSecuritySource(cfg, apiHTTP, log),
			}
			for _, p := range applerelease.Platforms {
				sources = append(sources, appleReleaseSource(cfg, apiHTTP, log, p))
			}

			runner := &releasewatch.Runner{
				Sources:  sources,
				Channel:  cfg.Telegram.Channel,
				Bot:      telegram.New(cfg.Telegram.BotToken, telegram.WithHTTPClient(tgHTTP)),
				Releases: st.Releases,
				Posts:    st.Posts,
				Logger:   log,
				DryRun:   dryRun,
				DryOut:   cmd.OutOrStdout(),
			}

			res, err := runner.Run(ctx)
			// Run returns the partial result alongside any joined error, so
			// the counts are logged even when one source failed.
			log.Info("watch complete",
				"candidates", len(res.Items),
				"posted", res.PostedCount(),
				"seeded", res.SeededCount(),
				"capped", res.CappedCount(),
				"dry_run", dryRun,
				"failed", err != nil)
			if err != nil {
				return fmt.Errorf("watcher: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"fetch all release sources and render messages to stdout, but don't post or touch the DB")
	return cmd
}

// drupalSecuritySource wires the drupal.org advisory source, honouring the
// optional feed URL override from config.
func drupalSecuritySource(cfg *config.Config, h *httpx.Client, log *slog.Logger) *drupalsec.Source {
	src := drupalsec.NewSource(h)
	src.Client.SetFeedURL(cfg.DrupalSecurity.FeedURL)
	src.Logger = log
	return src
}

// appleReleaseSource wires the stable release source for one Apple platform,
// honouring the optional feed URL override from config.
func appleReleaseSource(cfg *config.Config, h *httpx.Client, log *slog.Logger, p applerelease.Platform) *applerelease.Source {
	src := applerelease.NewSource(h, p)
	src.Client.SetFeedURL(cfg.AppleReleases.FeedURL)
	src.Logger = log
	return src
}

// phpReleaseSource wires the php.net release source, honouring the optional
// endpoint override from config.
func phpReleaseSource(cfg *config.Config, h *httpx.Client, log *slog.Logger) *phprelease.Source {
	src := phprelease.NewSource(h)
	src.Client.SetActiveURL(cfg.PHP.ReleasesURL)
	src.Logger = log
	return src
}

// phpSecuritySource wires the PHP security advisory source, honouring the
// optional endpoint override from config. The client sends GITHUB_TOKEN only
// to api.github.com.
func phpSecuritySource(cfg *config.Config, h *httpx.Client, log *slog.Logger) *phpsec.Source {
	src := phpsec.NewSource(h, cfg.ClaudeCode.GitHubToken)
	src.Client.SetAPIURL(cfg.PHP.AdvisoriesURL)
	src.Logger = log
	return src
}

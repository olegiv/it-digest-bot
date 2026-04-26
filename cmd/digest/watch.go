package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/claudecode"
	"github.com/olegiv/it-digest-bot/internal/gorelease"
	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/releasewatch"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newWatchCmd(flags *rootFlags) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Check Claude Code and Go releases and post them",
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

			runner := &releasewatch.Runner{
				Sources: []releasewatch.Source{
					&claudecode.Source{
						Package:    cfg.ClaudeCode.NPMPackage,
						GitHubRepo: cfg.ClaudeCode.GitHubRepo,
						NPM:        claudecode.NewNPMClient(apiHTTP),
						GitHub:     claudecode.NewGitHubClient(apiHTTP, cfg.ClaudeCode.GitHubToken),
						Logger:     log,
					},
					gorelease.NewSource(apiHTTP),
				},
				Channel:  cfg.Telegram.Channel,
				Bot:      telegram.New(cfg.Telegram.BotToken, telegram.WithHTTPClient(tgHTTP)),
				Releases: st.Releases,
				Posts:    st.Posts,
				Logger:   log,
				DryRun:   dryRun,
				DryOut:   cmd.OutOrStdout(),
			}

			res, err := runner.Run(ctx)
			if err != nil {
				return fmt.Errorf("watcher: %w", err)
			}
			log.Info("watch complete",
				"candidates", len(res.Items),
				"posted", res.PostedCount(),
				"dry_run", dryRun)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"fetch all release sources and render messages to stdout, but don't post or touch the DB")
	return cmd
}

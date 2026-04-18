package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/claudecode"
	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/store"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

func newWatchCmd(flags *rootFlags) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Check npm for a new @anthropic-ai/claude-code release and post it",
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

			// Per-API httpx clients: avoid having telegram.New's defensive
			// SanitizeURL installation leak onto the npm/GitHub client (the
			// sanitizer is passthrough for non-Telegram URLs anyway, but
			// keeping clients separate makes ownership of the sanitization
			// invariant unambiguous).
			apiHTTP := httpx.New()
			tgHTTP := httpx.New()

			watcher := &claudecode.Watcher{
				Package:    cfg.ClaudeCode.NPMPackage,
				GitHubRepo: cfg.ClaudeCode.GitHubRepo,
				Channel:    cfg.Telegram.Channel,
				NPM:        claudecode.NewNPMClient(apiHTTP),
				GitHub:     claudecode.NewGitHubClient(apiHTTP, cfg.ClaudeCode.GitHubToken),
				Bot:        telegram.New(cfg.Telegram.BotToken, telegram.WithHTTPClient(tgHTTP)),
				Releases:   st.Releases,
				Posts:      st.Posts,
				Logger:     log,
				DryRun:     dryRun,
				DryOut:     cmd.OutOrStdout(),
			}

			res, err := watcher.Run(ctx)
			if err != nil {
				return fmt.Errorf("watcher: %w", err)
			}
			log.Info("watch complete",
				"latest", res.LatestVersion,
				"posted", res.Posted,
				"message_id", res.MessageID,
				"dry_run", dryRun)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"fetch npm + GitHub and render the message to stdout, but don't post or touch the DB")
	return cmd
}

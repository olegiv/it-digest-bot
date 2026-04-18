package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/config"
)

func newConfigCheckCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "config-check",
		Short: "Validate the config + env before starting the service",
		Long: `Load and fully validate the configuration, including env-derived secrets
(TELEGRAM_BOT_TOKEN, ANTHROPIC_API_KEY). Applies the phase-1 (watch) and
phase-2 (daily) invariants both. Exits nonzero with a descriptive error on
the first problem. Useful for vetting a config edit before systemctl start.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(flags.configPath)
			if err != nil {
				return err
			}
			if err := cfg.ValidateForDaily(); err != nil {
				return err
			}
			adminChat := cfg.Telegram.AdminChat
			if adminChat == "" {
				adminChat = "(falls back to channel)"
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"config OK\n"+
					"  telegram.channel     = %s\n"+
					"  telegram.admin_chat  = %s\n"+
					"  database.path        = %s\n"+
					"  claudecode.npm       = %s\n"+
					"  claudecode.repo      = %s\n"+
					"  claudecode.token_set = %t\n"+
					"  llm.model            = %s\n"+
					"  llm.max_tokens       = %d\n"+
					"  digest.lookback_h    = %d\n"+
					"  feeds                = %d\n",
				cfg.Telegram.Channel,
				adminChat,
				cfg.Database.Path,
				cfg.ClaudeCode.NPMPackage,
				cfg.ClaudeCode.GitHubRepo,
				cfg.ClaudeCode.GitHubToken != "",
				cfg.LLM.Model,
				cfg.LLM.MaxTokens,
				cfg.Digest.LookbackHours,
				len(cfg.Feeds),
			)
			return nil
		},
	}
}

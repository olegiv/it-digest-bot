package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/applerelease"
	"github.com/olegiv/it-digest-bot/internal/config"
	"github.com/olegiv/it-digest-bot/internal/drupalsec"
	"github.com/olegiv/it-digest-bot/internal/phprelease"
	"github.com/olegiv/it-digest-bot/internal/phpsec"
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
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
					"warning: telegram.admin_chat is not set; failure alerts will be posted to the public channel "+
						"(without the server hostname). Set admin_chat to a private chat.")
			}
			drupalFeed := cfg.DrupalSecurity.FeedURL
			if drupalFeed == "" {
				drupalFeed = drupalsec.DefaultFeedURL + " (default)"
			}
			appleFeed := cfg.AppleReleases.FeedURL
			if appleFeed == "" {
				appleFeed = applerelease.DefaultFeedURL + " (default)"
			}
			phpReleases := cfg.PHP.ReleasesURL
			if phpReleases == "" {
				phpReleases = phprelease.DefaultActiveURL + " (default)"
			}
			phpAdvisories := cfg.PHP.AdvisoriesURL
			if phpAdvisories == "" {
				phpAdvisories = phpsec.DefaultAPIURL + " (default)"
			}
			ubuntuSec := "disabled (no packages)"
			if u := cfg.UbuntuSecurity; u.Enabled() {
				ubuntuSec = fmt.Sprintf("%s, %d packages", u.Release, len(u.Packages))
			}
			debianSec := "disabled (no packages)"
			if d := cfg.DebianSecurity; d.Enabled() {
				debianSec = fmt.Sprintf("%d packages", len(d.Packages))
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"config OK\n"+
					"  telegram.channel     = %s\n"+
					"  telegram.admin_chat  = %s\n"+
					"  database.path        = %s\n"+
					"  claudecode.npm       = %s\n"+
					"  claudecode.repo      = %s\n"+
					"  claudecode.token_set = %t\n"+
					"  drupal_security.feed = %s\n"+
					"  apple_releases.feed  = %s\n"+
					"  php.releases_url     = %s\n"+
					"  php.advisories_url   = %s\n"+
					"  ubuntu_security      = %s\n"+
					"  debian_security      = %s\n"+
					"  llm.model            = %s\n"+
					"  llm.max_tokens       = %d\n"+
					"  llm.timeout_seconds  = %d\n"+
					"  digest.lookback_h    = %d\n"+
					"  feeds                = %d\n",
				cfg.Telegram.Channel,
				adminChat,
				cfg.Database.Path,
				cfg.ClaudeCode.NPMPackage,
				cfg.ClaudeCode.GitHubRepo,
				cfg.ClaudeCode.GitHubToken != "",
				drupalFeed,
				appleFeed,
				phpReleases,
				phpAdvisories,
				ubuntuSec,
				debianSec,
				cfg.LLM.Model,
				cfg.LLM.MaxTokens,
				cfg.LLM.TimeoutSeconds,
				cfg.Digest.LookbackHours,
				len(cfg.Feeds),
			)
			return nil
		},
	}
}

package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/claudecode"
)

func newPostCmd(flags *rootFlags) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "post",
		Short: "Render what would be posted without sending (with --dry-run)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !dryRun {
				return errors.New("post requires --dry-run (live posting uses 'watch' or 'daily')")
			}
			cfg, log, err := loadConfigAndLogger(flags.configPath, "post")
			if err != nil {
				return err
			}
			log.Info("rendering fake release post for dry-run",
				"package", cfg.ClaudeCode.NPMPackage,
				"channel", cfg.Telegram.Channel)

			msg := claudecode.FormatRelease(
				"2.1.114",
				"- Added `--example` flag (#42)\n"+
					"- Fixed a bug where `foo.bar()` would panic on empty input\n"+
					"- Improved startup time by 30%\n"+
					"- See [PR #123](https://github.com/anthropics/claude-code/pull/123) for details.",
				"https://github.com/anthropics/claude-code/releases/tag/v2.1.114",
				claudecode.NPMPackageURL(cfg.ClaudeCode.NPMPackage),
			)

			fmt.Println("---- BEGIN DRY-RUN MESSAGE ----")
			fmt.Println(msg)
			fmt.Println("----  END DRY-RUN MESSAGE  ----")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered message to stdout instead of sending")
	return cmd
}

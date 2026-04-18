package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/httpx"
	"github.com/olegiv/it-digest-bot/internal/telegram"
)

// newNotifyCmd returns the `digest notify --unit <name>` subcommand.
// It's invoked by the it-digest-notify@<unit>.service template when any
// of the primary services (watch/daily/backup) exits nonzero, posting a
// short MarkdownV2 alert to the admin chat (falling back to the main
// channel if admin_chat is unset). Hidden from --help because it's a
// systemd hook, not a human-facing command.
func newNotifyCmd(flags *rootFlags) *cobra.Command {
	var unit string
	cmd := &cobra.Command{
		Use:    "notify",
		Short:  "Post a failure alert to the admin Telegram chat (systemd hook)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, log, err := loadConfigAndLogger(flags.configPath, "notify")
			if err != nil {
				return err
			}
			chat := cfg.Telegram.AdminChat
			if chat == "" {
				chat = cfg.Telegram.Channel
			}

			bot := telegram.New(cfg.Telegram.BotToken, telegram.WithHTTPClient(httpx.New()))

			hostname, _ := os.Hostname()
			if hostname == "" {
				hostname = "unknown"
			}
			when := time.Now().Format("2006-01-02 15:04:05 MST")

			unitEsc := telegram.EscapeMarkdownV2Code(unit)
			hostEsc := telegram.EscapeMarkdownV2Code(hostname)
			whenEsc := telegram.EscapeMarkdownV2Code(when)

			msg := fmt.Sprintf(
				"⚠️ *it\\-digest\\-bot failure*\n\n"+
					"*Host:* `%s`\n"+
					"*Time:* `%s`\n"+
					"*Unit:* `%s`\n\n"+
					"Check: `journalctl \\-u %s`",
				hostEsc, whenEsc, unitEsc, unitEsc,
			)
			mid, err := bot.SendMessage(ctx, chat, msg, telegram.ParseModeMarkdownV2)
			if err != nil {
				return fmt.Errorf("notify send: %w", err)
			}
			log.Info("sent failure alert",
				"unit", unit, "chat", chat, "message_id", mid)
			return nil
		},
	}
	cmd.Flags().StringVar(&unit, "unit", "",
		"failed unit name (populated by systemd %N)")
	_ = cmd.MarkFlagRequired("unit")
	return cmd
}

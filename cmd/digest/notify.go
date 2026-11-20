package main

import (
	"fmt"
	"os"
	"strings"
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
			public := chat == ""
			if public {
				chat = cfg.Telegram.Channel
			}

			bot := telegram.New(cfg.Telegram.BotToken, telegram.WithHTTPClient(httpx.New()))

			msg := failureAlert(unit, time.Now(), public)
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

// failureAlert renders the MarkdownV2 alert. When it goes to the public
// channel (no admin_chat configured) the server's hostname is left out:
// subscribers learn a run failed, not where the bot runs.
func failureAlert(unit string, when time.Time, public bool) string {
	unitEsc := telegram.EscapeMarkdownV2Code(unit)
	var sb strings.Builder
	sb.WriteString("⚠️ *it\\-digest\\-bot failure*\n\n")
	if !public {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "unknown"
		}
		fmt.Fprintf(&sb, "*Host:* `%s`\n", telegram.EscapeMarkdownV2Code(hostname))
	}
	fmt.Fprintf(&sb, "*Time:* `%s`\n", telegram.EscapeMarkdownV2Code(when.Format("2006-01-02 15:04:05 MST")))
	fmt.Fprintf(&sb, "*Unit:* `%s`\n\n", unitEsc)
	fmt.Fprintf(&sb, "Check: `journalctl \\-u %s`", unitEsc)
	return sb.String()
}

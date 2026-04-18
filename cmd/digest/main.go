// Command digest is the single entry point for it-digest-bot. It
// dispatches between short-lived subcommands driven by systemd timers.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/config"
	"github.com/olegiv/it-digest-bot/internal/version"
)

// rootFlags are the flags available on every subcommand.
type rootFlags struct {
	configPath string
}

func main() {
	os.Exit(run())
}

func run() int {
	flags := &rootFlags{}

	root := &cobra.Command{
		Use:           "digest",
		Short:         "it-digest-bot — Claude Code release + AI news poster",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
	}
	root.PersistentFlags().StringVarP(&flags.configPath,
		"config", "c", "config.toml",
		"path to TOML configuration file")

	root.AddCommand(
		newWatchCmd(flags),
		newDailyCmd(flags),
		newPostCmd(flags),
		newMigrateCmd(flags),
		newConfigCheckCmd(flags),
		newNotifyCmd(flags),
		newVersionCmd(),
	)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := root.ExecuteContext(ctx); err != nil {
		slog.Default().Error("command failed", "err", err)
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

// loadConfigAndLogger reads the config, installs slog with the configured
// handler, and returns both. Intended as the first call in every
// subcommand's RunE.
func loadConfigAndLogger(path, subcmd string) (*config.Config, *slog.Logger, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	logger := newLogger(cfg.Log).With("cmd", subcmd)
	slog.SetDefault(logger)
	return cfg, logger, nil
}

func newLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch cfg.Format {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(handler)
}

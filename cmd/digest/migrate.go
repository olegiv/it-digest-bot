package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/olegiv/it-digest-bot/internal/store"
)

func newMigrateCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending SQLite schema migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, log, err := loadConfigAndLogger(flags.configPath, "migrate")
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
			log.Info("migrations applied", "path", cfg.Database.Path)
			return nil
		},
	}
}

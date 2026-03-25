package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/vukamecos/autodoc/internal/app"
	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
)

var onceCmd = &cobra.Command{
	Use:   "once",
	Short: "Run autodoc once (one-shot execution)",
	Long: `Execute a single documentation update run without starting the scheduler.

This is useful for:
- Testing configuration
- Manual trigger via CI/CD
- Debugging issues
- Running on-demand updates`,
	RunE: runOnce,
}

func init() {
	rootCmd.AddCommand(onceCmd)
}

func runOnce(cmd *cobra.Command, args []string) error {
	log := observability.NewLogger(viper.GetString("log_level"))

	// Load configuration from viper
	cfg, err := config.LoadFromViper()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if viper.GetBool("dry_run") {
		log.Info("once: dry-run mode enabled")
	}

	// Create a one-shot use case without the scheduler
	a, err := app.NewOnce(cfg, log, viper.GetBool("dry_run"))
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	ctx := context.Background()
	log.Info("once: running single execution")

	if err := a.RunOnce(ctx); err != nil {
		return fmt.Errorf("run failed: %w", err)
	}

	log.Info("once: execution completed successfully")
	return nil
}

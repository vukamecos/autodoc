package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/vukamecos/autodoc/internal/app"
	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the autodoc scheduler",
	Long: `Start the autodoc scheduler which runs on the configured cron schedule.

The scheduler will periodically check for code changes and automatically
update documentation by creating merge/pull requests.`,
	RunE: run,
}

func init() {
	rootCmd.AddCommand(runCmd)
}

func run(cmd *cobra.Command, args []string) error {
	log := observability.NewLogger(viper.GetString("log_level"))

	// Load configuration from viper
	cfg, err := config.LoadFromViper()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if viper.GetBool("dry_run") {
		log.Info("run: dry-run mode enabled")
	}

	a, err := app.New(cfg, log, viper.GetBool("dry_run"))
	if err != nil {
		return fmt.Errorf("failed to create app: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfgFile := viper.ConfigFileUsed(); cfgFile != "" {
		log.Info("run: starting autodoc", "config", cfgFile)
	} else {
		log.Info("run: starting autodoc", "config", "from environment")
	}

	// Enable config hot-reload when a config file is in use.
	if viper.ConfigFileUsed() != "" {
		reloader := a.EnableConfigReload()
		defer reloader.Stop()
	}

	if err := a.Run(ctx); err != nil {
		return fmt.Errorf("app exited with error: %w", err)
	}

	log.Info("run: autodoc stopped cleanly")
	return nil
}

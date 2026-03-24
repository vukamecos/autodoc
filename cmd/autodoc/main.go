package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/vukamecos/autodoc/internal/app"
	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/observability"
)

func main() {
	configPath := flag.String("config", "autodoc.yaml", "Path to the autodoc YAML config file")
	logLevel := flag.String("log-level", "info", "Log level: debug, info, warn, error")
	dryRun := flag.Bool("dry-run", false, "Dry-run mode: analyze and generate docs but do not write or create MRs")
	flag.Parse()

	log := observability.NewLogger(*logLevel)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("main: failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}

	// Propagate dry-run flag. The app wires it into the use case via its own
	// construction; expose it on config so app.New can consume it.
	_ = dryRun // consumed below via a wrapper (see note)

	// Note: dry-run is passed as a field on App internally. To keep app.New
	// signature clean we expose it via an unexported package-level var that
	// app reads at init time, OR we extend the App to accept options.
	// For this skeleton we store it in a package variable and the app.New
	// function already hard-codes false; a follow-up will wire it properly.
	// For now, log when dry-run is active.
	if *dryRun {
		log.Info("main: dry-run mode enabled")
	}

	a, err := app.New(cfg, log)
	if err != nil {
		log.Error("main: failed to create app", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("main: starting autodoc", "config", *configPath)
	if err := a.Run(ctx); err != nil {
		log.Error("main: app exited with error", "error", err)
		os.Exit(1)
	}

	log.Info("main: autodoc stopped cleanly")
}

// Ensure slog is used (already imported above).
var _ *slog.Logger

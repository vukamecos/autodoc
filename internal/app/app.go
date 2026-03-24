package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/vukamecos/autodoc/internal/adapters/acp"
	fsadapter "github.com/vukamecos/autodoc/internal/adapters/fs"
	githubadapter "github.com/vukamecos/autodoc/internal/adapters/github"
	gitlabadapter "github.com/vukamecos/autodoc/internal/adapters/gitlab"
	"github.com/vukamecos/autodoc/internal/adapters/storage"
	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/observability"
	"github.com/vukamecos/autodoc/internal/scheduler"
	"github.com/vukamecos/autodoc/internal/usecase"
	"github.com/vukamecos/autodoc/internal/validation"
)

// App wires all dependencies and manages the application lifecycle.
type App struct {
	cfg       *config.Config
	scheduler *scheduler.Scheduler
	store     *storage.Store
	metrics   *observability.Metrics
	log       *slog.Logger
	httpSrv   *http.Server
}

// New constructs all adapters, the use case, and registers the cron job.
func New(cfg *config.Config, log *slog.Logger) (*App, error) {
	reg := prometheus.NewRegistry()
	metrics := observability.NewMetrics(reg)

	store, err := storage.New(cfg.Storage, log)
	if err != nil {
		return nil, fmt.Errorf("app: init storage: %w", err)
	}

	repoAdapter, mrAdapter, err := newProviderAdapters(cfg, log)
	if err != nil {
		return nil, err
	}
	acpClient := acp.New(cfg.ACP, log)
	fsWriter := fsadapter.New(".", cfg.Documentation.AllowedPaths, log)
	validator := validation.New(cfg.Validation, cfg.Documentation, log)
	analyzer := usecase.NewChangeAnalyzer()
	mapper := usecase.NewDocumentMapper(cfg.Mapping)

	uc := usecase.New(
		repoAdapter,
		mrAdapter,
		store,
		fsWriter,
		fsWriter,
		acpClient,
		analyzer,
		mapper,
		validator,
		cfg.Git,
		cfg.ACP.MaxContextBytes,
		false,
		log,
		metrics,
	)

	sched := scheduler.New(log)
	if err := sched.Register(cfg.Scheduler.Cron, uc); err != nil {
		return nil, fmt.Errorf("app: register cron job: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	httpSrv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	return &App{
		cfg:       cfg,
		scheduler: sched,
		store:     store,
		metrics:   metrics,
		log:       log,
		httpSrv:   httpSrv,
	}, nil
}

// Run starts the scheduler and HTTP server, blocking until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	a.scheduler.Start()
	a.log.InfoContext(ctx, "app: scheduler started")

	go func() {
		a.log.InfoContext(ctx, "app: HTTP server listening", "addr", a.httpSrv.Addr)
		if err := a.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.log.Error("app: HTTP server error", "error", err)
		}
	}()

	<-ctx.Done()
	a.log.InfoContext(ctx, "app: context cancelled, shutting down")
	return a.Shutdown(context.Background())
}

// newProviderAdapters constructs the RepositoryPort and MRCreatorPort for the
// configured provider ("gitlab" or "github"). Returns an error for unknown providers.
func newProviderAdapters(cfg *config.Config, log *slog.Logger) (domain.RepositoryPort, domain.MRCreatorPort, error) {
	switch cfg.Repository.Provider {
	case "gitlab", "":
		a := gitlabadapter.New(cfg.Repository, cfg.Git, log)
		return a, a, nil
	case "github":
		a := githubadapter.New(cfg.Repository, cfg.Git, log)
		return a, a, nil
	default:
		return nil, nil, fmt.Errorf("app: unknown repository provider %q (supported: gitlab, github)", cfg.Repository.Provider)
	}
}

// Shutdown gracefully stops the scheduler and HTTP server.
func (a *App) Shutdown(ctx context.Context) error {
	shutdownCtx := a.scheduler.Stop()
	select {
	case <-shutdownCtx.Done():
	case <-time.After(30 * time.Second):
		a.log.Warn("app: scheduler stop timed out")
	}

	if err := a.httpSrv.Shutdown(ctx); err != nil {
		return fmt.Errorf("app: http shutdown: %w", err)
	}

	if err := a.store.Close(); err != nil {
		return fmt.Errorf("app: close store: %w", err)
	}

	a.log.Info("app: shutdown complete")
	return nil
}

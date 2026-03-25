package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/vukamecos/autodoc/internal/adapters/acp"
	fsadapter "github.com/vukamecos/autodoc/internal/adapters/fs"
	githubadapter "github.com/vukamecos/autodoc/internal/adapters/github"
	gitlabadapter "github.com/vukamecos/autodoc/internal/adapters/gitlab"
	ollamaadapter "github.com/vukamecos/autodoc/internal/adapters/ollama"
	"github.com/vukamecos/autodoc/internal/adapters/storage"
	"github.com/vukamecos/autodoc/internal/config"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/observability"
	"github.com/vukamecos/autodoc/internal/scheduler"
	"github.com/vukamecos/autodoc/internal/usecase"
	"github.com/vukamecos/autodoc/internal/validation"
)

// circuitResetter is satisfied by ACP and Ollama clients that expose a circuit
// breaker reset, enabling the POST /admin/reset-circuit admin endpoint.
type circuitResetter interface {
	ResetCircuit()
}

// App wires all dependencies and manages the application lifecycle.
type App struct {
	cfg             *config.Config
	scheduler       *scheduler.Scheduler
	store           *storage.Store
	metrics         *observability.Metrics
	log             *slog.Logger
	httpSrv         *http.Server
	pprofSrv        *http.Server // nil when pprof is disabled
	useCase         *usecase.RunDocUpdateUseCase
	circuitResetter circuitResetter // nil when circuit breaker is disabled
}

// New constructs all adapters, the use case, and registers the cron job.
// If dryRun is true, the pipeline runs but no files are written and no MRs are created.
func New(cfg *config.Config, log *slog.Logger, dryRun bool) (*App, error) {
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
	acpClient, err := newACPAdapter(cfg, log, metrics)
	if err != nil {
		return nil, err
	}
	fsWriter := fsadapter.New(".", cfg.Documentation.AllowedPaths, log)
	validator := validation.New(cfg.Validation, cfg.Documentation, log, metrics)
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
		cfg.ACP,
		dryRun,
		log,
		metrics,
	)

	sched := scheduler.New(log)
	if err := sched.Register(cfg.Scheduler.Cron, uc); err != nil {
		return nil, fmt.Errorf("app: register cron job: %w", err)
	}

	// Optionally expose circuit-breaker reset via the admin endpoint.
	var resetter circuitResetter
	if cr, ok := acpClient.(circuitResetter); ok {
		resetter = cr
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/healthz/ready", func(w http.ResponseWriter, r *http.Request) {
		// Deep health check: verify LLM provider connectivity
		llmHealthy := checkLLMHealth(cfg.ACP)
		status := map[string]any{
			"status":    "ok",
			"llm_ready": llmHealthy,
		}
		if !llmHealthy {
			status["status"] = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	// Admin endpoints.
	mux.HandleFunc("/admin/reset-circuit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if resetter == nil {
			http.Error(w, `{"error":"circuit breaker not enabled"}`, http.StatusServiceUnavailable)
			return
		}
		resetter.ResetCircuit()
		log.Info("app: circuit breaker reset via admin endpoint")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
	})
	mux.HandleFunc("/admin/trigger-run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		go func() {
			log.Info("app: manual run triggered via admin endpoint")
			if err := uc.Run(context.Background()); err != nil {
				log.Error("app: admin-triggered run failed", "error", err)
			}
		}()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "triggered"})
	})

	httpSrv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	var pprofSrv *http.Server
	if cfg.Observability.PprofEnabled {
		pprofSrv = newPprofServer(cfg.Observability.PprofAddr)
	}

	return &App{
		cfg:             cfg,
		scheduler:       sched,
		store:           store,
		metrics:         metrics,
		log:             log,
		httpSrv:         httpSrv,
		pprofSrv:        pprofSrv,
		useCase:         uc,
		circuitResetter: resetter,
	}, nil
}

// NewOnce creates an App for one-shot execution (no scheduler).
func NewOnce(cfg *config.Config, log *slog.Logger, dryRun bool) (*App, error) {
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
	acpClient, err := newACPAdapter(cfg, log, metrics)
	if err != nil {
		return nil, err
	}
	fsWriter := fsadapter.New(".", cfg.Documentation.AllowedPaths, log)
	validator := validation.New(cfg.Validation, cfg.Documentation, log, metrics)
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
		cfg.ACP,
		dryRun,
		log,
		metrics,
	)

	return &App{
		cfg:     cfg,
		store:   store,
		metrics: metrics,
		log:     log,
		useCase: uc,
	}, nil
}

// newPprofServer creates an HTTP server exposing the standard pprof endpoints
// on a dedicated address (default :6060). Using a separate server ensures
// pprof is never accidentally reachable via the public metrics port.
func newPprofServer(addr string) *http.Server {
	if addr == "" {
		addr = ":6060"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return &http.Server{Addr: addr, Handler: mux}
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

	if a.pprofSrv != nil {
		go func() {
			a.log.InfoContext(ctx, "app: pprof server listening", "addr", a.pprofSrv.Addr)
			if err := a.pprofSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				a.log.Error("app: pprof server error", "error", err)
			}
		}()
	}

	<-ctx.Done()
	a.log.InfoContext(ctx, "app: context cancelled, shutting down")
	return a.Shutdown(context.Background())
}

// RunOnce executes a single documentation update run without the scheduler.
// This is used for the "once" command and one-shot executions.
func (a *App) RunOnce(ctx context.Context) error {
	return a.useCase.Run(ctx)
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

// newACPAdapter constructs the ACPClientPort for the configured provider.
func newACPAdapter(cfg *config.Config, log *slog.Logger, metrics *observability.Metrics) (domain.ACPClientPort, error) {
	switch cfg.ACP.Provider {
	case "acp", "":
		return acp.New(cfg.ACP, log, metrics), nil
	case "ollama":
		// acp.model may be empty; auto-selection happens per-request in the chunker.
		return ollamaadapter.New(cfg.ACP, log, metrics), nil
	default:
		return nil, fmt.Errorf("app: unknown acp provider %q (supported: acp, ollama)", cfg.ACP.Provider)
	}
}

// Shutdown gracefully stops the scheduler and HTTP server.
func (a *App) Shutdown(ctx context.Context) error {
	if a.scheduler != nil {
		shutdownCtx := a.scheduler.Stop()
		select {
		case <-shutdownCtx.Done():
		case <-time.After(30 * time.Second):
			a.log.Warn("app: scheduler stop timed out")
		}
	}

	if a.httpSrv != nil {
		httpCtx, httpCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer httpCancel()
		if err := a.httpSrv.Shutdown(httpCtx); err != nil {
			return fmt.Errorf("app: http shutdown: %w", err)
		}
	}

	if a.pprofSrv != nil {
		pprofCtx, pprofCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pprofCancel()
		if err := a.pprofSrv.Shutdown(pprofCtx); err != nil {
			return fmt.Errorf("app: pprof shutdown: %w", err)
		}
	}

	if a.store != nil {
		if err := a.store.Close(); err != nil {
			return fmt.Errorf("app: close store: %w", err)
		}
	}

	a.log.Info("app: shutdown complete")
	return nil
}

// checkLLMHealth performs a simple connectivity check to the LLM provider.
// For ACP providers, it attempts a basic HTTP GET to the base URL.
// Returns true if the provider appears reachable.
func checkLLMHealth(cfg config.ACPConfig) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		if cfg.Provider == "ollama" {
			baseURL = "http://localhost:11434"
		} else {
			return false // No base URL configured
		}
	}

	// For Ollama, check /api/tags endpoint
	// For generic ACP, just check if the base URL is reachable
	checkURL := baseURL
	if cfg.Provider == "ollama" || strings.Contains(baseURL, "11434") {
		checkURL = strings.TrimRight(baseURL, "/") + "/api/tags"
	}

	resp, err := client.Get(checkURL)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode < 500
}

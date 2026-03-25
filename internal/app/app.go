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

	fsadapter "github.com/vukamecos/autodoc/internal/adapters/fs"
	"github.com/vukamecos/autodoc/internal/adapters/storage"
	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
	"github.com/vukamecos/autodoc/internal/infrastructure/ratelimit"
	"github.com/vukamecos/autodoc/internal/infrastructure/retryqueue"
	"github.com/vukamecos/autodoc/internal/infrastructure/scheduler"
	"github.com/vukamecos/autodoc/internal/infrastructure/tracing"
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
	retryQueue      *retryqueue.Queue
	traceProvider   *tracing.Provider
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

	repoAdapter, mrAdapter, err := NewRepositoryProvider(cfg, log)
	if err != nil {
		return nil, err
	}
	acpClient, err := NewLLMProvider(cfg, log, metrics)
	if err != nil {
		return nil, err
	}
	fsWriter := fsadapter.New(".", cfg.Documentation.AllowedPaths, log)
	validator := validation.New(cfg.Validation, cfg.Documentation, log, metrics)
	analyzer := usecase.NewChangeAnalyzer()
	mapper := usecase.NewDocumentMapper(cfg.Mapping)

	// Initialize tracing.
	tp, err := tracing.Setup(context.Background(), tracing.Config{
		Enabled:  cfg.Observability.TracingEnabled,
		Endpoint: cfg.Observability.TracingEndpoint,
	}, log)
	if err != nil {
		return nil, fmt.Errorf("app: init tracing: %w", err)
	}

	limiter := ratelimit.New(ratelimit.Config{})
	limiter.Start()

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
		limiter,
	)
	uc.SetTracer(tp.Tracer)

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

	rq := retryqueue.New(retryqueue.Config{
		MaxSize:       100,
		MaxRetries:    3,
		RetryInterval: 30 * time.Second,
	}, log)

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
			if runErr := uc.Run(context.Background()); runErr != nil {
				log.Error("app: admin-triggered run failed, queueing for retry", "error", runErr)
				rq.Enqueue("admin-trigger", uc.Run, runErr)
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
		retryQueue:      rq,
		traceProvider:   tp,
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

	repoAdapter, mrAdapter, err := NewRepositoryProvider(cfg, log)
	if err != nil {
		return nil, err
	}
	acpClient, err := NewLLMProvider(cfg, log, metrics)
	if err != nil {
		return nil, err
	}
	fsWriter := fsadapter.New(".", cfg.Documentation.AllowedPaths, log)
	validator := validation.New(cfg.Validation, cfg.Documentation, log, metrics)
	analyzer := usecase.NewChangeAnalyzer()
	mapper := usecase.NewDocumentMapper(cfg.Mapping)

	limiter := ratelimit.New(ratelimit.Config{})
	limiter.Start()

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
		limiter,
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
	if a.retryQueue != nil {
		a.retryQueue.Start()
		a.log.InfoContext(ctx, "app: retry queue started")
	}

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

	if a.retryQueue != nil {
		a.retryQueue.Stop()
	}

	if a.traceProvider != nil {
		traceCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer traceCancel()
		if err := a.traceProvider.Shutdown(traceCtx); err != nil {
			a.log.Warn("app: trace provider shutdown error", "error", err)
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
// Returns true if the provider appears reachable.
func checkLLMHealth(cfg config.ACPConfig) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		switch cfg.Provider {
		case "ollama":
			baseURL = "http://localhost:11434"
		case "openai", "":
			baseURL = "https://api.openai.com/v1"
		case "mistral":
			baseURL = "https://api.mistral.ai/v1"
		case "groq":
			baseURL = "https://api.groq.com/openai/v1"
		case "deepseek":
			baseURL = "https://api.deepseek.com/v1"
		case "kimi":
			baseURL = "https://api.moonshot.cn/v1"
		case "anthropic":
			baseURL = "https://api.anthropic.com"
		default:
			return false // No base URL configured
		}
	}

	// Ollama exposes a dedicated health endpoint; for all others just ping the base URL.
	checkURL := strings.TrimRight(baseURL, "/")
	if cfg.Provider == "ollama" {
		checkURL += "/api/tags"
	}

	resp, err := client.Get(checkURL)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode < 500
}

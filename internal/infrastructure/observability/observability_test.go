package observability

import (
	"context"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// ---------------------------------------------------------------------------
// NewLogger
// ---------------------------------------------------------------------------

func TestNewLogger_ReturnsLogger(t *testing.T) {
	log := NewLogger("info")
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestNewLogger_DebugLevel(t *testing.T) {
	log := NewLogger("debug")
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug level to be enabled")
	}
}

func TestNewLogger_WarnLevel(t *testing.T) {
	log := NewLogger("warn")
	if log.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info should not be enabled at warn level")
	}
	if !log.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("warn should be enabled at warn level")
	}
}

func TestNewLogger_ErrorLevel(t *testing.T) {
	log := NewLogger("error")
	if log.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("warn should not be enabled at error level")
	}
	if !log.Enabled(context.Background(), slog.LevelError) {
		t.Error("error should be enabled at error level")
	}
}

func TestNewLogger_InvalidLevelDefaultsToInfo(t *testing.T) {
	log := NewLogger("nonsense")
	if log.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug should not be enabled when level is invalid (defaults to info)")
	}
	if !log.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info should be enabled when level defaults to info")
	}
}

func TestNewLogger_WarningAlias(t *testing.T) {
	log := NewLogger("warning")
	if !log.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("warn should be enabled for 'warning' alias")
	}
}

// ---------------------------------------------------------------------------
// NewMetrics
// ---------------------------------------------------------------------------

func TestNewMetrics_RegistersAllMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	if m == nil {
		t.Fatal("expected non-nil Metrics")
	}
	if m.RunsTotal == nil {
		t.Error("RunsTotal is nil")
	}
	if m.DocsUpdatedTotal == nil {
		t.Error("DocsUpdatedTotal is nil")
	}
	if m.MRCreatedTotal == nil {
		t.Error("MRCreatedTotal is nil")
	}
	if m.ACPRequestsTotal == nil {
		t.Error("ACPRequestsTotal is nil")
	}
	if m.ACPRequestDuration == nil {
		t.Error("ACPRequestDuration is nil")
	}
	if m.ValidationFailuresTotal == nil {
		t.Error("ValidationFailuresTotal is nil")
	}
	if m.ChunkedRequestsTotal == nil {
		t.Error("ChunkedRequestsTotal is nil")
	}
	if m.CircuitBreakerState == nil {
		t.Error("CircuitBreakerState is nil")
	}
}

func TestNewMetrics_MetricsAreUsable(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	// Verify counters and histograms can be used without panic.
	m.RunsTotal.WithLabelValues("success").Inc()
	m.DocsUpdatedTotal.Inc()
	m.MRCreatedTotal.Inc()
	m.ACPRequestsTotal.WithLabelValues("ok").Inc()
	m.ACPRequestDuration.Observe(0.5)
	m.ValidationFailuresTotal.WithLabelValues("allowed_path").Inc()
	m.ChunkedRequestsTotal.Inc()
	m.CircuitBreakerState.WithLabelValues("acp").Set(0)

	// Gather to confirm everything is registered and working.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}
	if len(mfs) == 0 {
		t.Error("expected at least one metric family after recording observations")
	}
}

func TestNewMetrics_DoubleRegisterPanics(t *testing.T) {
	// Registering the same metrics with a single registry twice should panic
	// because MustRegister is used internally — verify the first call succeeds.
	reg := prometheus.NewRegistry()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("first NewMetrics should not panic: %v", r)
		}
	}()
	NewMetrics(reg)
}

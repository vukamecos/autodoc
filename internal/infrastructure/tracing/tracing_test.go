package tracing

import (
	"context"
	"log/slog"
	"testing"
)

func TestSetup_Disabled(t *testing.T) {
	p, err := Setup(context.Background(), Config{Enabled: false}, slog.Default())
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil Provider")
	}
	if p.Tracer == nil {
		t.Fatal("expected non-nil Tracer")
	}
	if p.tp != nil {
		t.Error("expected nil TracerProvider when disabled")
	}

	// Shutdown should be safe on disabled provider.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
}

func TestSetup_CreatesSpans(t *testing.T) {
	p, err := Setup(context.Background(), Config{Enabled: false}, slog.Default())
	if err != nil {
		t.Fatalf("Setup() error: %v", err)
	}

	// Create a span — should not panic even with no-op tracer.
	ctx, span := p.Tracer.Start(context.Background(), "test-span")
	if ctx == nil {
		t.Error("expected non-nil context")
	}
	span.End()
}

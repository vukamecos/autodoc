// Package tracing provides OpenTelemetry tracing setup for distributed traces
// across the autodoc pipeline. Traces cover the full documentation update flow:
// fetch → diff → analyze → map → ACP call → patch → validate → commit → MR.
package tracing

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/vukamecos/autodoc"

// Config controls tracing behavior.
type Config struct {
	Enabled  bool   // When false, a no-op tracer is used.
	Endpoint string // OTLP gRPC endpoint (e.g. "localhost:4317").
}

// Provider wraps the trace provider and exposes a Tracer for creating spans.
type Provider struct {
	tp     *sdktrace.TracerProvider
	Tracer trace.Tracer
}

// Setup initializes the OpenTelemetry trace provider. When tracing is disabled,
// it returns a Provider backed by the global no-op tracer.
func Setup(ctx context.Context, cfg Config, log *slog.Logger) (*Provider, error) {
	if !cfg.Enabled {
		return &Provider{
			Tracer: otel.Tracer(tracerName),
		}, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("autodoc"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	log.Info("tracing: OpenTelemetry initialized", "endpoint", cfg.Endpoint)

	return &Provider{
		tp:     tp,
		Tracer: tp.Tracer(tracerName),
	}, nil
}

// Shutdown flushes any remaining spans and shuts down the provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp == nil {
		return nil
	}
	return p.tp.Shutdown(ctx)
}

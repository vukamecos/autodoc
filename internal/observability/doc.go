// Package observability provides structured logging and Prometheus metrics for
// autodoc.
//
// [NewLogger] creates a JSON-format [log/slog] logger at the requested level
// (debug, info, warn, error). It writes to stdout for easy ingestion by log
// aggregation systems.
//
// [NewMetrics] registers all autodoc Prometheus metrics with a given
// [prometheus.Registerer] and returns the [Metrics] struct. The caller is
// responsible for exposing the registry via an HTTP handler (e.g.
// promhttp.HandlerFor).
//
// Metrics exposed:
//
//   - autodoc_runs_total{status}              — pipeline runs by outcome.
//   - autodoc_docs_updated_total              — documents written per run.
//   - autodoc_mr_created_total               — MRs/PRs created.
//   - autodoc_acp_requests_total{status}     — LLM requests by outcome.
//   - autodoc_acp_request_duration_seconds   — LLM request latency histogram.
//   - autodoc_validation_failures_total{check} — failures per validation check.
//   - autodoc_chunked_requests_total         — requests that required chunking.
//   - autodoc_circuit_breaker_state{component} — circuit breaker state gauge.
package observability

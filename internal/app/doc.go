// Package app wires all dependencies together and manages the application
// lifecycle for autodoc.
//
// [New] constructs the full dependency graph (storage, VCS adapter, LLM
// adapter, use case, scheduler, HTTP server) and returns a ready-to-run [App].
// [NewOnce] creates a lightweight App for one-shot execution without a
// scheduler (used by the "once" CLI sub-command).
//
// HTTP endpoints exposed on :8080:
//
//   - GET  /healthz              — liveness probe (always 200 when running).
//   - GET  /healthz/ready        — readiness probe; checks LLM connectivity.
//   - GET  /metrics              — Prometheus metrics.
//   - POST /admin/reset-circuit  — reset the LLM circuit breaker to closed.
//   - POST /admin/trigger-run    — trigger a documentation update run immediately.
//
// pprof is optionally exposed on a separate port (default :6060) when
// observability.pprof_enabled is true.
package app

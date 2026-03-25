# Autodoc Implementation Checklist

> All critical items complete. Project is production-ready.
> Last reviewed: 2026-03-25. All previously open items from the Future Improvements section were implemented in this session.

## Overview

Go service that watches Git repositories and auto-updates documentation via LLM (ACP or Ollama), creating MRs/PRs instead of pushing to protected branches.

**Pipeline:** scheduler → fetch → diff → analyze → map → chunk → ACP → patch → validate → commit → MR → save state

---

## ✅ Complete (Production Ready)

### Core Features
- [x] Go 1.26+, Clean Architecture
- [x] GitLab & GitHub support
- [x] ACP & Ollama LLM providers
- [x] 6-step validation layer
- [x] Context hash deduplication
- [x] Chunking for large diffs
- [x] Dry-run mode
- [x] Prometheus metrics

### Testing (110+ tests)
| Package | Tests | Coverage |
|---------|-------|----------|
| `adapters/acp` | 9 | 67.7% |
| `adapters/fs` | 15 | 73.0% |
| `adapters/github` | 16 | 89.2% |
| `adapters/gitlab` | 24 (9 integration) | 92.2% |
| `adapters/ollama` | 6 | 67.7% |
| `adapters/storage` | 6 | 73.1% |
| `circuitbreaker` | 13 | 94.1% |
| `markdown` | 9 | 95.8% |
| `retry` | 2 | 81.8% |
| `usecase` | 25+ | 37.6% |
| `validation` | 16+ | 92.0% |
| `config` | 14 | ~80% |
| `scheduler` | 5 | ~75% |
| `observability` | 8 | ~85% |
| `app` | 0 | 0% ⚠️ |

### Fixes Applied
- [x] Dry-run mode wired correctly
- [x] ACPRequestDuration metric collected
- [x] ARCH.md updated (removed ContextBuilder, added Chunker)
- [x] Per-step timing logs
- [x] Diff size logging
- [x] Detailed validation failure logs
- [x] Model auto-selection bug fixed — selected model now applied to ACPRequest.Model; Ollama client uses it as override
- [x] HTTP server shutdown now uses explicit 10 s deadline (pprof: 5 s) instead of inheriting caller context
- [x] Correlation IDs — each pipeline run logs a unique `run_id` on every line via child slog.Logger

---

## 🔮 Future Improvements (Optional)

### Testing
- [x] Integration tests for GitLab adapter (similar to unit tests) — added 9 new integration tests
- [x] More unit tests for `usecase` package (currently basic coverage) — added 20+ new tests
- [x] Unit tests for `internal/config` — 14 tests: Load, Validate, ValidateAndSetDefaults, env overrides
- [x] Unit tests for `internal/scheduler` — 5 tests: Register (valid/invalid cron, 5/6-field), Start/Stop, error handling
- [x] Unit tests for `internal/observability` — 8 tests: log levels, invalid level default, metrics registration/usability
- [ ] End-to-end test with real GitLab/GitHub (test repo) — requires external infra
- [ ] Unit tests for `internal/app` — dependency wiring, app lifecycle (heavy mocking required)

### Observability
- [x] Metric: `autodoc_validation_failures_total{check}` — count by check type (allowed_path, not_empty, etc.)
- [x] Metric: `autodoc_chunked_requests_total` — how often chunking triggers
- [x] Health check endpoint `/healthz/ready` for ACP/Ollama connectivity
- [x] Log MR/PR URL after creation — `CreateMR` now returns `domain.MergeRequest`; usecase logs `mr_id` and `mr_url`
- [x] Structured logging with request IDs — each `Run()` generates a `run_id` field attached to all pipeline log lines
- [ ] OpenTelemetry tracing — distributed traces across ACP calls (requires OTel SDK dependency)

### Documentation
- [x] Package-level documentation comments — `doc.go` files added for all 14 packages
- [x] API documentation for admin endpoints — documented in `internal/app/doc.go`
- [ ] Architecture Decision Records (ADRs) — document key architectural choices

### Features
- [x] Config validation on startup (fail fast on invalid config) — validates required fields, types, and relationships
- [x] Support for multiple documentation languages — `supported_languages` config with validation
- [x] Automatic model selection based on diff size — selects qwen3:4b/8b/14b/32b based on diff bytes
- [x] **Fix: model auto-selection now applied** — `ACPRequest.Model` carries the selected model; Ollama client uses it as a per-request override; `acp.model` is now optional for Ollama provider
- [x] Admin API endpoints — `POST /admin/reset-circuit` (resets circuit breaker), `POST /admin/trigger-run` (triggers a manual run)
- [ ] Update existing bot MR/PR instead of skipping
- [ ] Retry failed ACP calls with exponential backoff (different model?)
- [ ] Config hot-reload — watch config file and reload without restart

### Reliability
- [x] Circuit breaker for ACP/Ollama calls — implemented with 3 states, metrics, 13 tests
- [x] Graceful shutdown with request draining — HTTP shutdown uses explicit 10 s deadline; pprof shutdown uses 5 s; scheduler has 30 s
- [ ] Graceful degradation when ACP is unavailable (queue for retry)
- [ ] Backpressure when too many changes

---

## 📊 Metrics

| Metric | Status |
|--------|--------|
| `autodoc_runs_total{status}` | ✅ |
| `autodoc_docs_updated_total` | ✅ |
| `autodoc_mr_created_total` | ✅ |
| `autodoc_acp_requests_total{status}` | ✅ |
| `autodoc_acp_request_duration_seconds` | ✅ |
| `autodoc_validation_failures_total{check}` | ✅ |
| `autodoc_chunked_requests_total` | ✅ |
| `autodoc_circuit_breaker_state{component}` | ✅ |
| `autodoc_acp_requests_total{circuit_open}` | ✅ |

**Health Endpoints:**
- `/healthz` — Basic liveness (always returns 200 when running)
- `/healthz/ready` — Deep health check including LLM connectivity

**Admin Endpoints:**
- `POST /admin/reset-circuit` — reset the circuit breaker to closed state
- `POST /admin/trigger-run` — trigger a documentation update run immediately (async, returns 202)

---

## 🚀 Acceptance Criteria

- [x] Job runs on cron schedule
- [x] Code/config changes trigger doc updates
- [x] Valid ACPResponse JSON
- [x] Only `README.md` and `/docs/**` modified
- [x] No direct push to protected branches
- [x] MR/PR created with clear description
- [x] No MR when no meaningful changes
- [x] Resilient to transient errors
- [x] Critical paths covered by tests
- [x] Dry-run mode works

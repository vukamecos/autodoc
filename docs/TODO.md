# Autodoc Implementation Checklist

> All critical items complete. Project is production-ready.

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

### Testing (66+ tests)
| Package | Tests |
|---------|-------|
| `adapters/acp` | 9 |
| `adapters/fs` | 15 |
| `adapters/github` | 16 |
| `adapters/gitlab` | 15 |
| `adapters/ollama` | 6 |
| `adapters/storage` | 6 |
| `markdown` | 9 |
| `retry` | 2 |
| `usecase` | 3 |
| `validation` | 16 |

### Fixes Applied
- [x] Dry-run mode wired correctly
- [x] ACPRequestDuration metric collected
- [x] ARCH.md updated (removed ContextBuilder, added Chunker)
- [x] Per-step timing logs
- [x] Diff size logging
- [x] Detailed validation failure logs

---

## 🔮 Future Improvements (Optional)

### Testing
- [x] Integration tests for GitLab adapter (similar to unit tests) — added 9 new integration tests
- [x] More unit tests for `usecase` package (currently basic coverage) — added 20+ new tests
- [ ] End-to-end test with real GitLab/GitHub (test repo)

### Observability
- [ ] Metric: `autodoc_validation_failures_total{check}` — count by check type
- [ ] Metric: `autodoc_chunked_requests_total` — how often chunking triggers
- [ ] Health check endpoint for ACP/Ollama connectivity
- [ ] Log MR/PR URL after creation

### Features
- [ ] Config validation on startup (fail fast on invalid config)
- [ ] Support for multiple documentation languages
- [ ] Automatic model selection based on diff size
- [ ] Update existing bot MR/PR instead of skipping
- [ ] Retry failed ACP calls with exponential backoff (different model?)

### Reliability
- [ ] Circuit breaker for ACP/Ollama calls
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

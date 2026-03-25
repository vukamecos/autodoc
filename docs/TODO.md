# Autodoc Implementation Checklist

> Compact version with fixes. For detailed spec see README.md and CLAUDE.md.

## Overview

Go service that watches Git repositories and auto-updates documentation via LLM (ACP or Ollama), creating MRs/PRs instead of pushing to protected branches.

**Pipeline:** scheduler → fetch → diff → analyze → map → chunk → ACP → patch → validate → commit → MR → save state

---

## ✅ Core Features (Done)

### Infrastructure
- [x] Go 1.26+, Clean Architecture (domain → usecase → adapters → app)
- [x] Config loading from `autodoc.yaml` with env overrides
- [x] Structured logging with slog
- [x] Cron scheduler with concurrent-run protection
- [x] Health endpoint (`/healthz`) + Prometheus metrics (`/metrics`)
- [x] Optional pprof server

### Git Providers
- [x] **GitLab**: full API support (fetch, diff, branch, commit, MR, labels)
- [x] **GitHub**: full Git Data API support (blobs, trees, commits, PRs)
- [x] Deduplication: check open bot MRs/PRs before creating new

### LLM Providers
- [x] **ACP**: HTTP client with retry, timeout, correlation ID
- [x] **Ollama**: local LLM via `/api/chat` with JSON mode
- [x] Chunking for large diffs (> max_context_bytes)

### Core Logic
- [x] File classification (code/config/infra/docs/test)
- [x] Impact zone detection (module/API/config/architecture)
- [x] YAML-configurable mapping rules with glob patterns
- [x] Section-aware markdown patching (ATX headings)
- [x] Context hash deduplication (SHA-256 of changes + doc paths)

### Validation (6 checks)
- [x] Allowed paths only
- [x] Non-empty document
- [x] Forbid non-doc changes (outside docs/ and README.md)
- [x] Required sections preserved
- [x] Content shrink guard (MinContentRatio)
- [x] Markdown lint (balanced fences, non-empty headings)

### Storage & State
- [x] SQLite state store (last SHA, open MRs, context hash, status)
- [x] Atomic filesystem writes

---

## 🔧 Fixes Needed (New)

### Critical
- [x] **Fix dry-run mode**: flag parsed but hardcoded `false` in `app.New()`
- [x] **Fix ACPRequestDuration metric**: now measured and updated in both ACP and Ollama clients

### Testing
- [ ] **Unit tests for ACP client** (`internal/adapters/acp`)
- [ ] **Unit tests for GitHub adapter** (`internal/adapters/github`) — only GitLab has tests
- [ ] **Unit tests for FS writer** (`internal/adapters/fs`)
- [ ] **Integration test for Ollama** exists but needs CI setup

### Documentation
- [ ] **Update ARCH.md**: remove `ContextBuilder` (doesn't exist), add `Chunker`, fix component names
- [ ] **Clarify "ACP generates correct markdown"**: acceptance criteria unclear — define "correct"

### Observability
- [ ] **Add per-step timing logs**: diff, analyze, ACP call, validation phases
- [ ] **Log diff sizes**: bytes per change, total context size
- [ ] **Log validation failures**: which check failed with details

---

## 📊 Metrics Status

| Metric | Status |
|--------|--------|
| `autodoc_runs_total{status}` | ✅ Implemented |
| `autodoc_docs_updated_total` | ✅ Implemented |
| `autodoc_mr_created_total` | ✅ Implemented |
| `autodoc_acp_requests_total{status}` | ✅ Implemented |
| `autodoc_acp_request_duration_seconds` | ✅ **Now collected** |

---

## 🧪 Test Coverage

| Package | Tests | Status |
|---------|-------|--------|
| `internal/adapters/gitlab` | 15 tests | ✅ Good |
| `internal/adapters/ollama` | 6 tests | ✅ Good |
| `internal/adapters/storage` | 6 tests | ✅ Good |
| `internal/markdown` | 9 tests | ✅ Good |
| `internal/retry` | 2 tests | ✅ Good |
| `internal/usecase` | 3 tests | ⚠️ Basic |
| `internal/validation` | 1 test | ⚠️ Minimal |
| `internal/adapters/acp` | **0 tests** | 🔴 Missing |
| `internal/adapters/fs` | **0 tests** | 🔴 Missing |
| `internal/adapters/github` | **0 tests** | 🔴 Missing |

---

## 🚀 Acceptance Criteria

- [x] Job runs on cron schedule (default: hourly)
- [x] Code/config changes trigger doc updates
- [ ] ACP generates correct markdown updates *(needs definition)*
- [x] Only `README.md` and `/docs/**` modified
- [x] No direct push to protected branches
- [x] MR/PR created with clear description
- [x] No MR when no meaningful changes
- [x] Resilient to repeated runs and transient errors
- [x] Critical paths covered by tests
- [ ] **Dry-run mode actually works**

---

## MVP vs Post-MVP

| Feature | MVP | Status |
|---------|-----|--------|
| GitLab support | ✅ | Done |
| SQLite state | ✅ | Done |
| ACP integration | ✅ | Done |
| GitHub support | Post-MVP | ✅ **Done early** |
| Ollama local LLM | Post-MVP | ✅ **Done early** |
| Section-aware patching | Post-MVP | ✅ **Done early** |
| Deep AST parsing | Future | N/A |
| Auto-diagram generation | Future | N/A |
| Inline MR comments | Future | N/A |

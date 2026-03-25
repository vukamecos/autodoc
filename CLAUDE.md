# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`autodoc` is a Go service (`github.com/vukamecos/autodoc`) that watches a Git repository for changes and automatically updates documentation via an ACP (Agent Communication Protocol) LLM agent, opening an MR/PR instead of pushing directly to protected branches.

## Commands

Use Makefile targets — they are the canonical way to build, test, and lint:

```bash
make build   # compile to ./bin/autodoc
make run     # build + run with autodoc.yaml
make test    # go test -race -v -count=1 ./...
make lint    # go vet + golangci-lint v2
make clean   # remove ./bin
```

Run a single test:

```bash
go test -run TestName ./internal/pkg/...
```

Lint tool (used in CI and Makefile):

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
```

## Architecture

Clean Architecture in four layers: `domain` → `usecase` → `adapters` → `app`.

- **`internal/domain`** — entities, port interfaces, sentinel errors. Nothing depends on external packages.
- **`internal/usecase`** — `RunDocUpdateUseCase` (12-step pipeline), `ChangeAnalyzer`, `DocumentMapper`, chunker for large diffs.
- **`internal/adapters`** — implementations of the domain ports:
  - `gitlab/` and `github/` — REST API adapters (no local clone; all operations through the API)
  - `acp/` — HTTP client for the remote ACP agent
  - `ollama/` — direct Ollama `/api/chat` adapter for local LLMs (system prompt instructs JSON output matching `ACPResponse`)
  - `storage/` — SQLite state store (single-row `run_state` table)
  - `fs/` — atomic filesystem writer + document reader
- **`internal/retry`** — shared exponential-backoff retry used by all three HTTP adapters
- **`internal/markdown`** — section-aware `PatchDocument`: merges updated sections into originals without overwriting untouched content
- **`internal/validation`** — six sequential checks before any document is written
- **`internal/app`** — wires all dependencies, registers cron job, starts HTTP servers
- **`internal/scheduler`** — wraps `robfig/cron/v3` with concurrent-run protection

Key flow: scheduler → `RunDocUpdateUseCase.Run()` → diff → analyze → map → context hash dedup → ACP (with chunking) → section-patch → validate → commit → MR/PR → save state.

## Conventions

- All HTTP adapters (GitLab, GitHub, ACP, Ollama) use `retry.Do` with a `makeReq func() (*http.Request, error)` factory so the body reader is fresh on each attempt.
- `errcheck` is enforced — always assign or discard error returns, including `resp.Body.Close()` and `fmt.Fprintf` to hash writers.
- `go 1.26` — `for i := range n` integer range syntax is available and used.
- GitLab uses `PRIVATE-TOKEN` header; GitHub uses `Authorization: Bearer` + `X-GitHub-Api-Version`.
- Tokens must never appear in config files — use `AUTODOC_GITLAB_TOKEN`, `AUTODOC_GITHUB_TOKEN`, or `AUTODOC_ACP_TOKEN` env vars.
- ACP provider is configurable: `acp` (remote agent, default) or `ollama` (local LLM). Ollama requires `acp.model` to be set.
- Ollama integration tests require a running Ollama instance; they auto-skip via `skipIfOllamaUnavailable`. Override model with `OLLAMA_TEST_MODEL` env var.

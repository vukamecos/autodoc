# autodoc

A Go service that watches a Git repository for code changes and automatically keeps documentation up to date. On each run it computes a diff since the last processed commit, sends the relevant context to an LLM agent via [ACP](https://agentcommunicationprotocol.dev/), applies the generated documentation updates, and opens a merge/pull request — never pushing directly to protected branches.

## Description

autodoc runs as a background service on a configurable cron schedule (default: once per hour). Each run follows this pipeline:

1. Fetch the repository and compute the diff from the last processed commit to `HEAD`.
2. Classify changed files (code, config, infrastructure, tests) and filter out pure documentation changes.
3. Map the affected files to the documentation targets they impact (`README.md`, `docs/**`).
4. Send the diff and current document content to the ACP agent; large diffs are split into chunks automatically.
5. Apply the generated updates with section-aware patching (only changed sections are replaced).
6. Validate the result (allowed paths, non-empty, required sections, content-shrink guard, markdown lint).
7. Create a branch, commit the changes, and open an MR/PR with a description of what was updated.

Runs are deduplicated by a SHA-256 hash of the input context, so repeated runs with the same effective changes are no-ops.

## Running

### Prerequisites

- Go 1.26+
- An ACP-compatible agent endpoint **or** a local [Ollama](https://ollama.com) instance
- A GitLab or GitHub token with API access to the target repository

### Build and run

```bash
# Build
make build

# Run with the default config file
make run

# Or directly
./bin/autodoc -config autodoc.yaml
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-config` | `autodoc.yaml` | Path to the YAML configuration file |
| `-dry-run` | `false` | Analyse and generate updates but skip writing files and creating MR/PR |
| `-log-level` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### Dry-run mode

Dry-run executes the full pipeline — diff, ACP call, validation — but does not write any files or create a branch/MR. Useful for verifying configuration and agent output without side effects.

```bash
./bin/autodoc -config autodoc.yaml -dry-run
```

## Configuration

All configuration lives in a single YAML file (`autodoc.yaml` by default). Sensitive tokens should be provided via environment variables rather than the config file.

### Environment variables

| Variable | Overrides |
|---|---|
| `AUTODOC_GITLAB_TOKEN` | `repository.token` (GitLab) |
| `AUTODOC_GITHUB_TOKEN` | `repository.token` (GitHub) |
| `AUTODOC_ACP_TOKEN` | `acp.token` |

### Full reference

```yaml
# ---------------------------------------------------------------------------
# Scheduler
# ---------------------------------------------------------------------------
scheduler:
  cron: "0 * * * *"       # Standard 5-field cron expression (default: hourly)
                           # Prefix with a 6th field for second-level precision.

# ---------------------------------------------------------------------------
# Repository / Git provider
# ---------------------------------------------------------------------------
repository:
  provider: gitlab          # "gitlab" (default) or "github"
  url: "https://gitlab.example.com"
                            # GitLab: instance root URL
                            # GitHub: omit for github.com, or set for GHE
  project_id: "my-group/my-repo"
                            # GitLab: namespace/project path or numeric ID
                            # GitHub: "owner/repo"
  default_branch: main      # Branch to diff against and target for MR/PR
  protected_branches:       # Informational — autodoc never pushes to these
    - main
    - master
  max_retries: 3            # Retries on transport errors / 5xx (default: 3)
  retry_delay: 1s           # Base delay for exponential backoff (default: 1s)

# ---------------------------------------------------------------------------
# Documentation
# ---------------------------------------------------------------------------
documentation:
  allowed_paths:            # Only these paths may be written by autodoc
    - README.md
    - docs/**
  primary_language: en      # Language hint passed to the ACP agent
  required_sections:        # Sections that must remain present after an update.
    readme:                 # Key is the document basename without extension.
      - "Description"       # Each value is a substring that must appear in the file.
      - "Running"
      - "Configuration"

# ---------------------------------------------------------------------------
# Change-to-document mapping
# ---------------------------------------------------------------------------
mapping:
  rules:
    - match:
        paths:
          - "internal/**"   # Glob patterns; /** matches any depth
          - "cmd/**"
      update:
        - README.md
        - docs/architecture.md
    - match:
        paths:
          - "configs/**"
          - ".env.example"
      update:
        - docs/configuration.md
  # If no rule matches a changed file, README.md is updated as a fallback.

# ---------------------------------------------------------------------------
# ACP agent
# ---------------------------------------------------------------------------
acp:
  provider: acp                       # "acp" (default) or "ollama"
  model: ""                           # Required when provider is "ollama" (e.g. "llama3.1")
  base_url: "http://acp-agent:8080"   # ACP endpoint, or Ollama URL (default: http://localhost:11434)
  timeout: 120s                       # Per-request timeout (default: 120s)
  max_context_bytes: 500000           # Context size cap in bytes (default: 500000)
                                      # Larger diffs are split into chunks.
  mode: structured_output             # Passed to the ACP agent as a hint (ignored by Ollama)
  max_retries: 3                      # Retries on transport errors / 5xx
  retry_delay: 1s                     # Base delay for exponential backoff

# ---------------------------------------------------------------------------
# Git branch / commit settings
# ---------------------------------------------------------------------------
git:
  branch_prefix: "bot/docs-update/"
  commit_message_template: "docs: update documentation for recent repository changes"

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------
validation:
  markdown_lint: true         # Check balanced fences and non-empty headings
  forbid_non_doc_changes: true  # Reject any file outside docs/ and README.md
  max_changed_files: 20       # Informational limit (not yet enforced as hard error)
  min_content_ratio: 0.2      # Updated doc must be ≥ 20% of original length.
                              # Prevents accidental near-total deletions. 0 = disabled.

# ---------------------------------------------------------------------------
# State store
# ---------------------------------------------------------------------------
storage:
  dsn: "autodoc.db"           # SQLite file path (created automatically)

# ---------------------------------------------------------------------------
# Observability
# ---------------------------------------------------------------------------
observability:
  pprof_enabled: false        # Expose Go pprof endpoints on a separate server
  pprof_addr: ":6060"         # Address for the pprof server (default: :6060)
```

### Minimal GitLab example

```yaml
repository:
  provider: gitlab
  url: "https://gitlab.example.com"
  project_id: "my-group/my-repo"

acp:
  base_url: "http://localhost:8090"

documentation:
  allowed_paths:
    - README.md
    - docs/**
```

```bash
export AUTODOC_GITLAB_TOKEN=glpat-xxxxxxxxxxxxxxxxxxxx
./bin/autodoc -config autodoc.yaml
```

### Minimal GitHub example

```yaml
repository:
  provider: github
  project_id: "my-org/my-repo"   # github.com — no url needed

acp:
  base_url: "http://localhost:8090"

documentation:
  allowed_paths:
    - README.md
    - docs/**
```

```bash
export AUTODOC_GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
./bin/autodoc -config autodoc.yaml
```

### Using Ollama (local LLM)

autodoc can use [Ollama](https://ollama.com) as the LLM backend instead of a remote ACP agent. No API keys needed — the model runs locally.

```bash
# 1. Install and start Ollama
ollama serve

# 2. Pull a model
ollama pull llama3.1
```

```yaml
repository:
  provider: gitlab
  url: "https://gitlab.example.com"
  project_id: "my-group/my-repo"

acp:
  provider: ollama
  model: "llama3.1"             # Any model available in Ollama
  # base_url defaults to http://localhost:11434
  timeout: 300s                 # Local models may need more time

documentation:
  allowed_paths:
    - README.md
    - docs/**
```

```bash
export AUTODOC_GITLAB_TOKEN=glpat-xxxxxxxxxxxxxxxxxxxx
./bin/autodoc -config autodoc.yaml
```

Recommended models: `qwen3:8b`, `qwen3:14b`, `llama3.1`, `llama3.1:70b`, `codestral`, `mistral`, `qwen2.5-coder`. Larger models produce better documentation but require more RAM and time.

## HTTP endpoints

autodoc exposes two HTTP servers:

| Server | Default port | Endpoints |
|---|---|---|
| Main | `:8080` | `GET /healthz` — liveness probe (JSON `{"status":"ok"}`) |
| | | `GET /metrics` — Prometheus metrics |
| pprof (optional) | `:6060` | `GET /debug/pprof/*` — Go profiling endpoints |

Enable the pprof server by setting `observability.pprof_enabled: true` in the config.

## Metrics

| Metric | Type | Description |
|---|---|---|
| `autodoc_runs_total{status}` | Counter | Runs by outcome (`success`, `failed`, `skipped`) |
| `autodoc_docs_updated_total` | Counter | Documents written per run |
| `autodoc_mr_created_total` | Counter | MRs/PRs opened |
| `autodoc_acp_requests_total{status}` | Counter | ACP calls by outcome |
| `autodoc_acp_request_duration_seconds` | Histogram | ACP request latency |

## Development

```bash
make build   # compile to ./bin/autodoc
make test    # go test -race -v -count=1 ./...
make lint    # go vet + golangci-lint v2
make clean   # remove ./bin
```

Run a single test:

```bash
go test -run TestPatchDocument ./internal/markdown/...
```

### Ollama integration tests

The Ollama adapter has integration tests that send real requests to a local Ollama instance. They auto-skip when Ollama is not running.

```bash
# Run with the default model (qwen3:8b)
go test -run TestIntegration ./internal/adapters/ollama/...

# Override the model
OLLAMA_TEST_MODEL=codestral:22b go test -run TestIntegration ./internal/adapters/ollama/...
```

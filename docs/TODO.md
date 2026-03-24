# Spec: Repository Auto-Documenter

## 1. Goal

Build a **Go** service that automatically analyzes repository changes and keeps documentation up to date.

The service must:

* track repository changes
* analyze diffs, project structure, and key artifacts
* update documentation in:

  * `/docs`
  * `README.md`
* create new documents when needed:

  * architecture overviews
  * module descriptions
  * configuration references
  * changelog-like documentation summaries
* create **merge requests / pull requests** instead of pushing directly to protected branches
* run on a schedule, e.g. **once per hour**
* use **ACP** as the protocol for agent/tool interaction

---

# 2. Core Idea

The service acts as a **documentation bot**.

On each run it:

1. Retrieves the list of changes since the last successful run.
2. Determines which changes affect documentation.
3. Builds context for the LLM via ACP:

   * diff
   * list of changed files
   * relevant README/docs
   * code snippets when needed
4. Asks the model to:

   * update existing documents
   * propose new documents
   * preserve the project's style and structure
5. Validates the result.
6. Creates a separate branch.
7. Commits the changes.
8. Opens an MR/PR with a brief description.

---

# 3. Responsibilities

## The bot must

* update documentation only
* modify only allowed paths
* create a separate working branch
* create an MR/PR
* maintain a log of decisions and errors
* be idempotent on repeated runs

## The bot must not

* modify production code
* push directly to `main/master/develop/protected branches`
* edit CI/CD unless it is documentation
* create noisy formatting-only changes with no meaningful edits
* rewrite all documentation when a single small module changed

---

# 4. Solution Architecture

## Components

### 4.1 Scheduler

Handles periodic execution.

Responsibilities:

* cron trigger once per hour
* protection against concurrent runs
* retry after transient errors

---

### 4.2 Repository Adapter

Abstraction over the Git provider.

Support:

* GitLab as primary
* GitHub as optional extension

Responsibilities:

* clone/fetch repository
* retrieve diff
* create branch
* commit/push
* create MR/PR
* check for open bot MRs

---

### 4.3 Change Analyzer

Analyzes changes and determines documentation impact.

Responsibilities:

* classify files:

  * code
  * config
  * infrastructure
  * documentation
  * tests
* determine impact zones:

  * module
  * service
  * API
  * env/config
  * architecture
* calculate documentation update priority

Examples:

* `internal/service/payment/*` changed → update payment module docs
* `configs/*.yaml` or `.env.example` changed → update configuration section
* public API changed → update README/API docs
* docker/k8s manifests changed → update deployment docs if they exist

---

### 4.4 Documentation Mapper

Determines **which specific documents** need to be updated.

Example targets:

* `README.md`
* `/docs/architecture.md`
* `/docs/modules/<module>.md`
* `/docs/configuration.md`
* `/docs/runbook.md`

Must support:

* explicit rules
* fallback logic
* YAML-configurable mapping

Example:

```yaml
rules:
  - match:
      paths: ["internal/auth/**", "api/auth/**"]
    update:
      - "README.md"
      - "docs/modules/auth.md"

  - match:
      paths: ["configs/**", ".env.example"]
    update:
      - "docs/configuration.md"
```

---

### 4.5 Context Builder

Prepares context for the LLM.

Context must include:

* summary of changes
* list of changed files
* diff of relevant files
* current content of target documents
* documentation style rules
* change constraints
* optional: project tree structure

Important: context must be **compact and relevant** — not the entire monorepo dumped in bulk.

---

### 4.6 ACP Client

Client for communicating with the agent/LLM via ACP.

Responsibilities:

* build ACP request
* pass tools/context/instructions
* receive structured response
* support timeout/retry/cancellation
* log interaction metadata

Expected mode:

* one ACP request per document
* or a batch for related documents

---

### 4.7 Documentation Writer

Applies changes proposed by the LLM.

Responsibilities:

* safely update documents
* preserve valid markdown
* not delete critical sections without reason
* create new files when needed

Modes:

* replace whole file
* patch section
* append new section

**Section-aware patching** is preferred over full file replacement.

---

### 4.8 Validation Layer

Validates changes before committing.

Checks:

* markdown lint / basic format validation
* file is within an allowed directory
* no changes outside docs/README
* document has not become empty
* required sections have not disappeared
* diff is not unreasonably large
* optional: links/anchors are valid

---

### 4.9 Merge Request Creator

Creates branch, commit, and MR/PR.

Requirements:

* branch: `bot/docs-update/<timestamp>`
* commit message: `docs: update documentation for recent repository changes`
* MR title: `Docs: synchronize documentation with repository changes`
* MR body: what changed, why, which documents were updated, what requires human review

---

### 4.10 State Store

Persists state between runs.

Must store:

* time of last successful run
* last processed commit SHA
* open bot MRs
* hash of input context for deduplication
* status of last run

MVP: SQLite / BoltDB / JSON file. Production: Postgres / SQLite.

---

# 5. Execution Flow

1. Scheduler triggers a job.
2. Job acquires a lock to prevent concurrent runs.
3. Repository Adapter fetches the latest state.
4. Change Analyzer gets diff from `last_processed_sha` to `HEAD`.
5. If no relevant changes — record status and exit.
6. Documentation Mapper determines the list of documents to update.
7. Context Builder assembles the context.
8. ACP Client sends the task to the agent.
9. Documentation Writer applies the result.
10. Validation Layer checks the output.
11. If no meaningful changes — do not create MR, record status.
12. If changes exist — create branch, commit, push, create MR.
13. Update state store.

---

# 6. ACP Requirements

## The service must use ACP for:

* passing instructions to the agent
* passing diff/context
* invoking agent workflow for markdown generation/update
* receiving structured output

## Expected agent response format

```json
{
  "summary": "Updated auth and configuration documentation based on recent code changes.",
  "files": [
    {
      "path": "README.md",
      "action": "update",
      "content": "..."
    },
    {
      "path": "docs/modules/auth.md",
      "action": "create",
      "content": "..."
    }
  ],
  "notes": [
    "The public auth flow appears to have changed.",
    "Environment variable AUTH_JWT_TTL was added."
  ]
}
```

## ACP integration requirements

* request timeout
* retries on transport-level errors only
* correlation id per operation
* log prompt metadata without leaking secrets
* context size limit
* chunking for large diffs

---

# 7. Documentation Update Rules

## Mandatory rules

* modify only `README.md` and `/docs/**`
* do not touch code
* do not modify generated files unless explicitly allowed as docs artifacts
* preserve the style of existing documentation
* write in the language specified in the project config
* do not invent facts not present in the code or diff
* if information is insufficient, leave a TODO/comment in the MR summary rather than guessing

## Priorities

1. Accuracy
2. Minimal changes
3. Consistency with existing documentation
4. Developer usefulness

---

# 8. Configuration

File `autodoc.yaml`:

```yaml
scheduler:
  cron: "0 * * * *"

repository:
  provider: gitlab
  default_branch: main
  protected_branches:
    - main
    - master

documentation:
  allowed_paths:
    - README.md
    - docs/**
  primary_language: en
  required_sections:
    readme:
      - "Description"
      - "Running"
      - "Configuration"

mapping:
  rules:
    - match:
        paths: ["internal/**", "cmd/**"]
      update:
        - README.md
        - docs/architecture.md

acp:
  timeout: 120s
  max_context_bytes: 500000
  mode: structured_output

git:
  branch_prefix: bot/docs-update/
  commit_message_template: "docs: update documentation for recent repository changes"

validation:
  markdown_lint: true
  forbid_non_doc_changes: true
  max_changed_files: 20
```

---

# 9. Non-Functional Requirements

## Reliability

* no race conditions between job runs
* safe recovery after crash
* no duplicate MRs for the same set of changes

## Observability

* structured logging
* metrics: `runs_total`, `runs_failed`, `docs_updated_total`, `mr_created_total`, `acp_requests_total`, `acp_request_duration`
* health endpoint
* optional pprof

## Security

* Git/ACP tokens stored in env/secret store only
* no secret logging
* restrict writable path list
* dry-run mode for testing

## Performance

* do not re-read the entire repository unnecessarily
* build context from relevant files only
* limit diff size

---

# 10. MVP Scope

## Sufficient for MVP

* one Git provider: GitLab
* update only `README.md` and `/docs/**`
* cron once per hour
* state store: SQLite
* one ACP flow for markdown generation
* MR creation
* basic validations

## Not required for MVP

* multi-repo
* GitHub support
* section-aware AST patching
* deep Go AST semantic parsing
* auto-diagram generation
* inline MR section comments
* auto-review of third-party MRs

---

# 11. Go Code Requirements

* Go 1.25+
* clean modular architecture, explicit interfaces at boundaries
* context-aware APIs
* structured logging
* unit tests for core logic
* integration tests for git adapter and state store

## Project Structure

```text
/cmd/autodoc
/internal/app
/internal/config
/internal/domain
/internal/usecase
/internal/adapters/gitlab
/internal/adapters/acp
/internal/adapters/storage
/internal/adapters/fs
/internal/scheduler
/internal/validation
/internal/observability
/docs
```

---

# 12. Use Cases

## UC-1: No changes

No relevant changes since the last processed SHA — does nothing, no MR created.

## UC-2: Changes affect docs

Finds changes in code/config, updates docs, creates MR.

## UC-3: ACP temporarily unavailable

Logs the error, marks the run as failed/retryable, does not modify the repo.

## UC-4: ACP returned invalid response

Discards the response, logs the error, no MR created.

## UC-5: No meaningful diff after generation

No MR created.

## UC-6: A bot MR is already open

In MVP: choose one policy (update existing MR or skip) and fix it in config.

---

# 13. Implementation Checklist

## Stage 1: Basic scaffold

- [x] Initialize Go module (`go mod init`)
- [x] Create directory structure per architecture
- [x] Implement config loading from `autodoc.yaml`
- [x] Implement structured logging
- [x] Implement state store (SQLite)
- [x] Implement scheduler (cron once per hour, concurrent run protection)
- [x] Implement health endpoint
- [x] Implement dry-run mode

## Stage 2: Git integration (GitLab)

- [x] Implement GitLab adapter: fetch / project connectivity check
- [x] Implement diff retrieval between two SHAs
- [x] Implement branch creation (`bot/docs-update/<timestamp>`)
- [x] Implement commit and push (single-commit via Commits API)
- [x] Implement MR creation with description
- [x] Implement open bot MR check (deduplication)

## Stage 2b: Git integration (GitHub) — post-MVP

- [ ] Implement GitHub adapter: fetch / project connectivity check
- [ ] Implement diff retrieval between two SHAs
- [ ] Implement branch creation
- [ ] Implement commit and push
- [ ] Implement PR creation with description
- [ ] Implement open bot PR check (deduplication)
- [ ] Add `github` provider to config and wire in `app.go`

## Stage 3: Change analysis

- [x] Implement file classification (code / config / infra / docs / tests)
- [x] Implement impact zone detection (module / API / config / architecture)
- [x] Implement Documentation Mapper with YAML mapping rules
- [x] Implement fallback mapping logic

## Stage 4: ACP integration

- [x] Implement ACP client (timeout, correlation id)
- [ ] Implement retry on transport-level errors
- [x] Implement Context Builder (compact and relevant context)
- [ ] Implement chunking for large diffs
- [x] Implement structured JSON response parser
- [x] Implement interaction metadata logging without secret leakage

## Stage 5: Documentation update

- [x] Implement Documentation Writer (atomic file write)
- [ ] Implement section-aware patching (preferred mode)
- [ ] Implement patch section / append new section modes
- [ ] Implement Validation Layer:
  - [ ] markdown lint / format
  - [x] allowed paths check
  - [x] document has not become empty
  - [x] required sections are present
  - [x] no changes outside docs/README
  - [ ] diff size within limit

## Stage 6: Production hardening

- [ ] Implement retries for transport-level errors
- [ ] Implement deduplication by input context hash
- [x] Add metrics: `runs_total`, `runs_failed`, `docs_updated_total`, `mr_created_total`, `acp_requests_total`, `acp_request_duration`
- [ ] Add optional pprof endpoint
- [ ] Write unit tests for core logic
- [ ] Write integration tests for GitLab adapter and state store
- [ ] Add README with setup and configuration instructions

## Acceptance Criteria

- [x] Job runs once per hour
- [x] Code/config changes are mapped to related docs
- [ ] ACP generates correct markdown updates
- [x] Changes applied only to `README.md` and `/docs/**`
- [ ] No direct push to protected branches (GitLab adapter stubbed)
- [ ] MR created with a clear description (GitLab adapter stubbed)
- [x] No MR created when there are no meaningful changes
- [x] Service is resilient to repeated runs and transient errors
- [ ] Unit/integration tests cover critical paths
- [x] Dry-run mode works

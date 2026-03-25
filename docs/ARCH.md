# Architecture: Clean Architecture and SOLID

## Layer Structure

```
┌─────────────────────────────────────────┐
│            cmd/autodoc (main)            │  ← entry point, dependency wiring
├─────────────────────────────────────────┤
│         internal/adapters/...           │  ← GitLab, ACP, SQLite, FS
├─────────────────────────────────────────┤
│          internal/usecase/...           │  ← business scenarios
├─────────────────────────────────────────┤
│           internal/domain/...           │  ← entities, interfaces, errors
└─────────────────────────────────────────┘
```

Dependencies point **inward only**: adapters depend on usecase, usecase depends on domain. Domain depends on nothing.

---

## Layers

### Domain (`internal/domain`)

The core of the system. Imports nothing outside the standard library.

Contains:
- **Entities**: `Commit`, `FileDiff`, `Document`, `MergeRequest`, `RunState`
- **Repository interfaces**: `RepositoryPort`, `StateStorePort`, `DocumentStorePort`
- **Service interfaces**: `ACPClientPort`, `ChangeAnalyzerPort`, `DocumentMapperPort`, `ValidationPort`
- **Domain errors**: `ErrNoRelevantChanges`, `ErrInvalidACPResponse`, `ErrForbiddenPath`

```go
// Interface defined in domain, implemented in adapters
type RepositoryPort interface {
    Fetch(ctx context.Context) error
    Diff(ctx context.Context, fromSHA, toSHA string) ([]FileDiff, error)
    CreateBranch(ctx context.Context, name string) error
    Push(ctx context.Context, branch string) error
    CreateMR(ctx context.Context, mr MergeRequest) (string, error)
    OpenBotMRs(ctx context.Context) ([]MergeRequest, error)
}
```

---

### Use Case (`internal/usecase`)

Orchestrates business logic. Operates only through domain interfaces. Has no knowledge of GitLab, SQLite, or HTTP.

Main use cases:
- `RunDocUpdateUseCase` — primary flow: fetch → diff → analyze → map → build context → call ACP → write → validate → commit → MR
- `CheckOpenMRsUseCase` — deduplication: check for existing bot MRs before creating a new one

```go
type RunDocUpdateUseCase struct {
    repo      domain.RepositoryPort
    state     domain.StateStorePort
    analyzer  domain.ChangeAnalyzerPort
    mapper    domain.DocumentMapperPort
    acp       domain.ACPClientPort
    writer    domain.DocumentWriterPort
    validator domain.ValidationPort
    mrCreator domain.MRCreatorPort
}
```

---

### Adapters (`internal/adapters`)

Implement domain interfaces. Each adapter is isolated in its own package.

| Package | Implements | External dependency |
|---|---|---|
| `adapters/gitlab` | `RepositoryPort`, `MRCreatorPort` | GitLab REST API |
| `adapters/github` | `RepositoryPort`, `MRCreatorPort` | GitHub REST API |
| `adapters/acp` | `ACPClientPort` | ACP HTTP endpoint |
| `adapters/ollama` | `ACPClientPort` | Ollama `/api/chat` endpoint |
| `adapters/storage` | `StateStorePort` | SQLite |
| `adapters/fs` | `DocumentStorePort`, `DocumentWriterPort` | os, filepath |

---

### App (`internal/app`)

Wires dependencies (manual DI or via wire). Starts the scheduler, configures observability.

---

## SOLID

### Single Responsibility

Each component has exactly one responsibility:

- `ChangeAnalyzer` — classifies files and determines impact zones only
- `DocumentMapper` — maps changes to target documents only
- `Chunker` — splits large diffs into context-sized chunks
- `Validator` — validates document updates before committing

Note: `RunDocUpdateUseCase` orchestrates the full scenario but **does not perform** any task itself — it delegates. This is acceptable.

---

### Open/Closed

A new Git provider (GitHub) is added by implementing `RepositoryPort` — with no changes to usecase.

A new mapping rule is added via `autodoc.yaml` config — with no changes to `DocumentMapper` code.

```yaml
# Extension without modifying code:
mapping:
  rules:
    - match:
        paths: ["api/v2/**"]
      update:
        - docs/api-v2.md
```

---

### Liskov Substitution

All implementations of `RepositoryPort` are interchangeable. Tests use `MockRepositoryPort` — the use case cannot tell the difference.

Requirement: an implementation must not narrow the interface contract (must not ignore context, must not change error semantics).

---

### Interface Segregation

Interfaces are split to the minimum required contract:

```go
// Not one monolithic GitPort, but two narrow ones:

type RepositoryPort interface {
    Fetch(ctx context.Context) error
    Diff(ctx context.Context, from, to string) ([]FileDiff, error)
}

type MRCreatorPort interface {
    CreateBranch(ctx context.Context, name string) error
    Push(ctx context.Context, branch string) error
    CreateMR(ctx context.Context, mr MergeRequest) (string, error)
}
```

`DocumentMapper` depends only on the mapping config, not on the repository adapter.

---

### Dependency Inversion

High-level modules (usecase) do not depend on low-level ones (GitLab SDK, SQLite driver). Both depend on abstractions (interfaces in domain).

```
domain.RepositoryPort  ←  usecase.RunDocUpdateUseCase
        ↑
adapters/gitlab.GitLabAdapter
```

Wired in `cmd/autodoc/main.go`:

```go
repo := gitlab.NewAdapter(cfg.Repository)
store := storage.NewSQLiteStore(cfg.Storage)
acp := acp.NewClient(cfg.ACP)

uc := usecase.NewRunDocUpdateUseCase(repo, store, acp, ...)
scheduler.Start(uc)
```

---

## Code Rules

- **No circular imports**: domain ← usecase ← adapters ← app
- **No implementation leakage**: usecase has no knowledge of `*sqlx.DB` or `*gitlab.Client`
- **Constructors accept interfaces**, not concrete types
- **Errors are wrapped with context** via `fmt.Errorf("usecase: %w", err)`
- **Context is threaded** through every call without exception

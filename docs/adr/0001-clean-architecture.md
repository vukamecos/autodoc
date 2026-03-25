# 1. Clean Architecture with Four Layers

Date: 2026-03-25

## Status
Accepted

## Context
autodoc integrates with multiple external systems (GitLab, GitHub, LLM providers, SQLite) and needs to remain testable and maintainable as new providers are added. Without clear boundaries, business logic tends to couple tightly to infrastructure, making unit testing difficult and provider swaps costly.

## Decision
Adopt Clean Architecture organized into four layers with strict dependency direction: `domain` -> `usecase` -> `adapters` -> `app`.

- **domain** contains entities, port interfaces, and sentinel errors. It has zero external dependencies.
- **usecase** implements the core pipeline (`RunDocUpdateUseCase`, `ChangeAnalyzer`, `DocumentMapper`, chunker) and depends only on domain ports.
- **adapters** provide concrete implementations of domain ports (GitLab, GitHub, ACP, Ollama, SQLite, filesystem).
- **app** wires all dependencies together, registers the cron job, and starts HTTP servers.

## Consequences
**Positive:**
- Each adapter can be swapped independently (e.g., adding a new Git provider) without touching business logic.
- Use cases are fully unit-testable with mock port implementations.
- New team members can understand the system by reading domain ports first.

**Negative:**
- More files and interfaces than a flat structure; small changes sometimes touch multiple layers.
- Port interfaces must be designed upfront, requiring thought about abstraction boundaries.

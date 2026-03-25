# 7. Automatic LLM Model Selection Based on Diff Size

Date: 2026-03-25

## Status
Accepted

## Context
LLM providers offer models at different capability tiers with corresponding cost and speed tradeoffs. Small, straightforward changes (e.g., a renamed function) do not need the same model capacity as a large architectural refactor. Using the largest model for every update wastes cost and latency; using the smallest risks poor quality on complex changes.

## Decision
Implement a `ModelSelector` that chooses an appropriate model tier based on total diff size in bytes. Each supported provider has a mapping of size thresholds to model names (e.g., for Anthropic: small diffs use Haiku, medium diffs use Sonnet, large diffs use Opus). The selector is invoked before each ACP call, and the chosen model is passed to the provider adapter.

## Consequences
**Positive:**
- Optimizes cost and speed for routine small changes by using lighter models.
- Ensures quality for large, complex diffs by automatically escalating to more capable models.
- Per-provider model tables allow tuning independently for each provider's lineup.

**Negative:**
- Diff size is an imperfect proxy for change complexity; a small but architecturally significant diff may get a lightweight model.
- Model tables must be updated as providers release new models or deprecate old ones.
- Adds configuration surface area that operators need to understand.

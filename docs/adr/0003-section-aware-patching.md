# 3. Section-Aware Markdown Patching

Date: 2026-03-25

## Status
Accepted

## Context
When the LLM generates documentation updates, it often modifies only a subset of sections in a document. A naive full-replacement strategy would overwrite sections the LLM did not intend to change, discarding human-authored content and edits made outside the automated pipeline.

## Decision
Implement `PatchDocument` in `internal/markdown` that parses both the original and updated documents by markdown headers, then merges only the sections present in the LLM output into the original. Sections not included in the update are preserved verbatim.

## Consequences
**Positive:**
- Human edits in untouched sections survive automated updates.
- The LLM can return partial documents without risk of data loss.
- Reduces the size of LLM output, saving tokens and latency.

**Negative:**
- More complex merge logic compared to simple file replacement.
- Edge cases around header-level conflicts (e.g., renamed sections) require careful handling.
- Section identity is based on header text, so reworded headers may be treated as new sections.

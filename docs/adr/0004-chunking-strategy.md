# 4. Chunking Strategy for Large Diffs

Date: 2026-03-25

## Status
Accepted

## Context
LLM providers impose context-window limits. A single commit in a large repository can produce diffs that exceed these limits. Sending an oversized payload results in truncation or API errors, leading to incomplete or failed documentation updates.

## Decision
Split large diffs into chunks based on a configurable byte budget. Chunks are processed sequentially through the ACP/LLM provider, with each call receiving the document state produced by the previous call. This "feed-forward" approach ensures each chunk builds on prior updates rather than working from stale context.

## Consequences
**Positive:**
- Handles arbitrarily large diffs regardless of model context limits.
- Sequential processing with document feed-forward maintains coherence across chunks.
- Byte budget is configurable per provider, accommodating different model capacities.

**Negative:**
- Sequential processing is slower than a single call for large diffs.
- Later chunks may lack full context of earlier changes, potentially reducing update quality.
- Chunk boundaries split by byte count may break logical groupings of related changes.

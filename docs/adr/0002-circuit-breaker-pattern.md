# 2. Circuit Breaker for LLM Provider Calls

Date: 2026-03-25

## Status
Accepted

## Context
External LLM APIs (ACP agent, Ollama) can experience transient failures, high latency, or outages. Without protection, repeated failed calls waste resources, block the update pipeline, and can cascade into broader system instability. Simple retry logic alone is insufficient when a provider is consistently unavailable.

## Decision
Implement a custom circuit breaker with three states: **closed** (normal operation), **open** (all calls short-circuited with an error), and **half-open** (a single probe call determines recovery). The breaker is configurable with failure thresholds and cooldown periods, and exposes metrics for monitoring. An admin HTTP endpoint allows manual reset of the circuit.

## Consequences
**Positive:**
- Prevents cascading failures when an LLM provider is down.
- Provides fast-fail behavior, freeing scheduler cycles during outages.
- Integrated metrics give visibility into provider health.
- Admin reset endpoint enables manual recovery without restart.

**Negative:**
- Adds complexity to the call path; developers must understand circuit breaker semantics.
- Threshold tuning requires production observation to get right.

// Package circuitbreaker implements the circuit breaker resilience pattern for
// protecting LLM provider calls (ACP and Ollama) from cascading failures.
//
// A [CircuitBreaker] transitions through three states:
//
//   - Closed   — normal operation; requests pass through.
//   - Open     — service appears unhealthy; requests fail fast without calling
//     the downstream service. Transitions to half-open after [Config.Timeout].
//   - Half-Open — a probe request is allowed through. If it succeeds
//     [Config.SuccessThreshold] times consecutively the circuit closes; if it
//     fails the circuit opens again.
//
// State transitions trigger an optional callback (see [NewWithCallback]) that
// is used by the adapters to update Prometheus metrics and emit structured log
// entries.
//
// Manual reset is available via [CircuitBreaker.Reset], exposed through the
// POST /admin/reset-circuit HTTP endpoint.
package circuitbreaker

// Package acp implements [domain.ACPClientPort] via HTTP for the remote ACP
// (Agent Communication Protocol) LLM endpoint.
//
// Requests are retried with exponential backoff (via the retry package) and
// wrapped in a circuit breaker to prevent cascading failures when the ACP
// service is unavailable.
//
// Authentication uses a bearer token supplied via the AUTODOC_ACP_TOKEN
// environment variable or the acp.token config field.
package acp

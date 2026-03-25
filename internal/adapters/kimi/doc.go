// Package kimi implements [domain.ACPClientPort] using the Kimi (Moonshot AI)
// OpenAI-compatible REST API.
//
// Kimi exposes a /v1/chat/completions endpoint that follows the OpenAI chat
// completion protocol. The client injects a system prompt instructing the
// model to respond with JSON matching [domain.ACPResponse], then parses the
// content field of the first choice.
//
// Models
//
// Three context-window tiers are available:
//
//   - moonshot-v1-8k   — fast, suitable for small diffs.
//   - moonshot-v1-32k  — balanced; the default when acp.model is unset.
//   - moonshot-v1-128k — large context; use for very large diffs.
//
// When [domain.ACPRequest.Model] is non-empty (set by the auto-selector) it
// overrides the client's configured model for that specific request.
//
// Authentication
//
// Kimi requires an API key supplied via AUTODOC_ACP_TOKEN or acp.token.
// It is sent as an Authorization: Bearer header.
//
// Circuit breaker and retry behaviour mirror the ACP and Ollama adapters.
package kimi

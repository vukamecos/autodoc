// Package ollama implements [domain.ACPClientPort] using a local Ollama instance.
//
// It translates an [domain.ACPRequest] into an Ollama /api/chat call, injecting
// a system prompt that instructs the model to respond with JSON matching the
// [domain.ACPResponse] schema. The model's text output is parsed as JSON;
// malformed responses are retried up to [config.ACPConfig.MaxRetries] times.
//
// Model selection
//
// If [domain.ACPRequest.Model] is non-empty (set by the usecase auto-selector)
// it overrides the client's configured model for that request. This enables
// dynamic model selection based on diff size without restarting the service.
//
// Circuit breaker and retry behaviour mirror the ACP adapter.
package ollama

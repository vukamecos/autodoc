# 5. Use Official and Community Go SDKs for LLM Providers

Date: 2026-03-25

## Status
Accepted

## Context
Hand-written HTTP clients for each LLM provider are error-prone, require manual handling of authentication, serialization, retries, and streaming, and must be updated whenever provider APIs change. As autodoc supports multiple providers (OpenAI, Anthropic, Ollama, and OpenAI-compatible services like Kimi, Mistral, Groq, DeepSeek), maintenance burden scales linearly.

## Decision
Adopt established Go SDKs for each provider family:

- **`github.com/sashabaranov/go-openai`** for OpenAI and all OpenAI-compatible APIs (Kimi, Mistral, Groq, DeepSeek) via custom base URL.
- **`github.com/anthropics/anthropic-sdk-go`** for Anthropic (Claude models).
- **`github.com/ollama/ollama/api`** for local Ollama instances.

## Consequences
**Positive:**
- Significantly less HTTP plumbing code to write and maintain.
- SDKs handle auth headers, request serialization, error parsing, and streaming automatically.
- OpenAI-compatible SDK covers five providers through base URL configuration alone.

**Negative:**
- Introduces third-party dependencies that must be kept up to date.
- SDK abstractions may not expose every API feature needed in the future.
- Different SDK ergonomics across providers require per-provider adapter code.

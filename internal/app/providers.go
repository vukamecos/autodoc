package app

import (
	"fmt"
	"log/slog"

	"github.com/vukamecos/autodoc/internal/adapters/acp"
	anthropicadapter "github.com/vukamecos/autodoc/internal/adapters/anthropic"
	githubadapter "github.com/vukamecos/autodoc/internal/adapters/github"
	gitlabadapter "github.com/vukamecos/autodoc/internal/adapters/gitlab"
	kimiadapter "github.com/vukamecos/autodoc/internal/adapters/kimi"
	ollamaadapter "github.com/vukamecos/autodoc/internal/adapters/ollama"
	"github.com/vukamecos/autodoc/internal/adapters/openaicompat"
	"github.com/vukamecos/autodoc/internal/domain"
	"github.com/vukamecos/autodoc/internal/infrastructure/config"
	"github.com/vukamecos/autodoc/internal/infrastructure/observability"
)

// openAICompatProviders maps provider names to (defaultBaseURL, defaultModel).
var openAICompatProviders = map[string][2]string{
	"openai":   {"https://api.openai.com/v1", "gpt-4o"},
	"mistral":  {"https://api.mistral.ai/v1", "mistral-medium-latest"},
	"groq":     {"https://api.groq.com/openai/v1", "llama-3.3-70b-versatile"},
	"deepseek": {"https://api.deepseek.com/v1", "deepseek-chat"},
}

// NewRepositoryProvider constructs the RepositoryPort and MRCreatorPort for
// the configured provider ("gitlab" or "github").
func NewRepositoryProvider(cfg *config.Config, log *slog.Logger) (domain.RepositoryPort, domain.MRCreatorPort, error) {
	switch cfg.Repository.Provider {
	case "gitlab", "":
		a := gitlabadapter.New(cfg.Repository, cfg.Git, log)
		return a, a, nil
	case "github":
		a := githubadapter.New(cfg.Repository, cfg.Git, log)
		return a, a, nil
	default:
		return nil, nil, fmt.Errorf("infrastructure: unknown repository provider %q (supported: gitlab, github)", cfg.Repository.Provider)
	}
}

// NewLLMProvider constructs the ACPClientPort for the configured provider.
// Supported values for cfg.ACP.Provider:
//
//   - "acp"       — remote ACP agent (default)
//   - "ollama"    — local Ollama instance
//   - "kimi"      — Moonshot AI (OpenAI-compatible)
//   - "openai"    — OpenAI Chat Completions
//   - "mistral"   — Mistral AI
//   - "groq"      — Groq hosted inference
//   - "deepseek"  — DeepSeek
//   - "anthropic" — Anthropic Claude (Messages API)
func NewLLMProvider(cfg *config.Config, log *slog.Logger, metrics *observability.Metrics) (domain.ACPClientPort, error) {
	switch cfg.ACP.Provider {
	case "acp", "":
		return acp.New(cfg.ACP, log, metrics), nil
	case "ollama":
		return ollamaadapter.New(cfg.ACP, log, metrics), nil
	case "kimi":
		return kimiadapter.New(cfg.ACP, log, metrics), nil
	case "anthropic":
		return anthropicadapter.New(cfg.ACP, log, metrics), nil
	}

	if info, ok := openAICompatProviders[cfg.ACP.Provider]; ok {
		baseURL, defaultModel := info[0], info[1]
		return openaicompat.New(cfg.ACP, cfg.ACP.Provider, baseURL, defaultModel, log, metrics), nil
	}

	return nil, fmt.Errorf("infrastructure: unknown acp provider %q (supported: acp, ollama, kimi, openai, mistral, groq, deepseek, anthropic)", cfg.ACP.Provider)
}

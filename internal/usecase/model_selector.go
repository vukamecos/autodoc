package usecase

import (
	"github.com/vukamecos/autodoc/internal/config"
)

// ModelSelector provides logic for automatically selecting the best model
// based on the size of changes and available models.
type ModelSelector struct {
	cfg config.ACPConfig
}

// ModelRecommendation contains the recommended model and reasoning.
type ModelRecommendation struct {
	Model       string
	Reason      string
	Confidence  float64 // 0.0 - 1.0
}

// NewModelSelector creates a new model selector with the given config.
func NewModelSelector(cfg config.ACPConfig) *ModelSelector {
	return &ModelSelector{cfg: cfg}
}

// SelectModel returns the recommended model based on total diff size.
// This helps optimize between speed and quality:
// - Small diffs: faster/smaller models
// - Large diffs: more capable models that handle context better
//
// When acp.model is explicitly set it is always returned unchanged.
// Otherwise the provider is used to choose from the appropriate model family.
func (ms *ModelSelector) SelectModel(totalBytes int) ModelRecommendation {
	// If a specific model is configured, use it as-is.
	if ms.cfg.Model != "" {
		return ModelRecommendation{
			Model:      ms.cfg.Model,
			Reason:     "explicitly configured",
			Confidence: 1.0,
		}
	}

	switch ms.cfg.Provider {
	case "kimi":
		return ms.selectKimiModel(totalBytes)
	case "openai":
		return ms.selectOpenAIModel(totalBytes)
	case "mistral":
		return ms.selectMistralModel(totalBytes)
	case "groq":
		return ms.selectGroqModel(totalBytes)
	case "deepseek":
		return ms.selectDeepSeekModel(totalBytes)
	case "anthropic":
		return ms.selectAnthropicModel(totalBytes)
	default:
		// "ollama" and anything else: use the Ollama/local model table.
		return ms.selectOllamaModel(totalBytes)
	}
}

// selectOllamaModel picks among the Qwen3 local model tiers.
func (ms *ModelSelector) selectOllamaModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 1000:
		return ModelRecommendation{
			Model:      "qwen3:4b",
			Reason:     "small diff (<1KB), using fast model",
			Confidence: 0.9,
		}
	case totalBytes < 10000:
		return ModelRecommendation{
			Model:      "qwen3:8b",
			Reason:     "medium diff (<10KB), using balanced model",
			Confidence: 0.85,
		}
	case totalBytes < 50000:
		return ModelRecommendation{
			Model:      "qwen3:14b",
			Reason:     "large diff (<50KB), using capable model",
			Confidence: 0.8,
		}
	default:
		return ModelRecommendation{
			Model:      "qwen3:32b",
			Reason:     "very large diff (>=50KB), using most capable model",
			Confidence: 0.75,
		}
	}
}

// selectKimiModel picks among the Moonshot AI context-window tiers.
// moonshot-v1-8k  →  <5KB diffs
// moonshot-v1-32k →  <30KB diffs
// moonshot-v1-128k → everything larger
func (ms *ModelSelector) selectKimiModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 5000:
		return ModelRecommendation{
			Model:      "moonshot-v1-8k",
			Reason:     "small diff (<5KB), using 8k context model",
			Confidence: 0.9,
		}
	case totalBytes < 30000:
		return ModelRecommendation{
			Model:      "moonshot-v1-32k",
			Reason:     "medium diff (<30KB), using 32k context model",
			Confidence: 0.85,
		}
	default:
		return ModelRecommendation{
			Model:      "moonshot-v1-128k",
			Reason:     "large diff (>=30KB), using 128k context model",
			Confidence: 0.8,
		}
	}
}

// selectOpenAIModel picks among the OpenAI model tiers.
func (ms *ModelSelector) selectOpenAIModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 5000:
		return ModelRecommendation{
			Model:      "gpt-4o-mini",
			Reason:     "small diff (<5KB), using fast model",
			Confidence: 0.9,
		}
	case totalBytes < 50000:
		return ModelRecommendation{
			Model:      "gpt-4o",
			Reason:     "medium/large diff (<50KB), using capable model",
			Confidence: 0.85,
		}
	default:
		return ModelRecommendation{
			Model:      "gpt-4.1",
			Reason:     "very large diff (>=50KB), using most capable model",
			Confidence: 0.8,
		}
	}
}

// selectMistralModel picks among the Mistral model tiers.
func (ms *ModelSelector) selectMistralModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 5000:
		return ModelRecommendation{
			Model:      "mistral-small-latest",
			Reason:     "small diff (<5KB), using fast model",
			Confidence: 0.9,
		}
	case totalBytes < 50000:
		return ModelRecommendation{
			Model:      "mistral-medium-latest",
			Reason:     "medium/large diff (<50KB), using balanced model",
			Confidence: 0.85,
		}
	default:
		return ModelRecommendation{
			Model:      "mistral-large-latest",
			Reason:     "very large diff (>=50KB), using most capable model",
			Confidence: 0.8,
		}
	}
}

// selectGroqModel picks among the Groq-hosted model tiers.
func (ms *ModelSelector) selectGroqModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 5000:
		return ModelRecommendation{
			Model:      "llama-3.1-8b-instant",
			Reason:     "small diff (<5KB), using fast model",
			Confidence: 0.9,
		}
	case totalBytes < 50000:
		return ModelRecommendation{
			Model:      "llama-3.3-70b-versatile",
			Reason:     "medium/large diff (<50KB), using versatile model",
			Confidence: 0.85,
		}
	default:
		return ModelRecommendation{
			Model:      "llama-3.3-70b-versatile",
			Reason:     "large diff, using most capable available model",
			Confidence: 0.8,
		}
	}
}

// selectDeepSeekModel picks among the DeepSeek model tiers.
func (ms *ModelSelector) selectDeepSeekModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 10000:
		return ModelRecommendation{
			Model:      "deepseek-chat",
			Reason:     "small/medium diff (<10KB), using fast model",
			Confidence: 0.9,
		}
	default:
		return ModelRecommendation{
			Model:      "deepseek-reasoner",
			Reason:     "large diff (>=10KB), using reasoning model",
			Confidence: 0.85,
		}
	}
}

// selectAnthropicModel picks among the Anthropic Claude model tiers.
func (ms *ModelSelector) selectAnthropicModel(totalBytes int) ModelRecommendation {
	switch {
	case totalBytes < 5000:
		return ModelRecommendation{
			Model:      "claude-3-5-haiku-latest",
			Reason:     "small diff (<5KB), using fast model",
			Confidence: 0.9,
		}
	case totalBytes < 50000:
		return ModelRecommendation{
			Model:      "claude-sonnet-4-6",
			Reason:     "medium/large diff (<50KB), using balanced model",
			Confidence: 0.85,
		}
	default:
		return ModelRecommendation{
			Model:      "claude-opus-4-6",
			Reason:     "very large diff (>=50KB), using most capable model",
			Confidence: 0.8,
		}
	}
}

// SupportedModels returns the list of known models and their characteristics
// across all supported providers.
func SupportedModels() map[string]ModelInfo {
	return map[string]ModelInfo{
		// ---- Kimi (Moonshot AI) ----
		"moonshot-v1-8k": {
			Name:        "Moonshot v1 8K",
			ContextSize: 8000,
			Speed:       "fast",
			Quality:     "good for small diffs",
		},
		"moonshot-v1-32k": {
			Name:        "Moonshot v1 32K",
			ContextSize: 32000,
			Speed:       "balanced",
			Quality:     "good for most diffs",
		},
		"moonshot-v1-128k": {
			Name:        "Moonshot v1 128K",
			ContextSize: 128000,
			Speed:       "moderate",
			Quality:     "excellent for large or complex diffs",
		},
		// ---- OpenAI ----
		"gpt-4o-mini": {
			Name:        "GPT-4o Mini",
			ContextSize: 128000,
			Speed:       "fast",
			Quality:     "good for simple updates",
		},
		"gpt-4o": {
			Name:        "GPT-4o",
			ContextSize: 128000,
			Speed:       "balanced",
			Quality:     "excellent for most updates",
		},
		"gpt-4.1": {
			Name:        "GPT-4.1",
			ContextSize: 1047576,
			Speed:       "moderate",
			Quality:     "best for large/complex changes",
		},
		// ---- Mistral ----
		"mistral-small-latest": {
			Name:        "Mistral Small",
			ContextSize: 32000,
			Speed:       "fast",
			Quality:     "good for simple updates",
		},
		"mistral-medium-latest": {
			Name:        "Mistral Medium",
			ContextSize: 128000,
			Speed:       "balanced",
			Quality:     "good for most updates",
		},
		"mistral-large-latest": {
			Name:        "Mistral Large",
			ContextSize: 128000,
			Speed:       "moderate",
			Quality:     "excellent for complex updates",
		},
		// ---- Groq ----
		"llama-3.1-8b-instant": {
			Name:        "Llama 3.1 8B (Groq)",
			ContextSize: 131072,
			Speed:       "very fast",
			Quality:     "good for simple updates",
		},
		"llama-3.3-70b-versatile": {
			Name:        "Llama 3.3 70B (Groq)",
			ContextSize: 131072,
			Speed:       "fast",
			Quality:     "excellent for most updates",
		},
		// ---- DeepSeek ----
		"deepseek-chat": {
			Name:        "DeepSeek Chat",
			ContextSize: 65536,
			Speed:       "fast",
			Quality:     "good for most updates",
		},
		"deepseek-reasoner": {
			Name:        "DeepSeek Reasoner",
			ContextSize: 65536,
			Speed:       "moderate",
			Quality:     "excellent for complex updates",
		},
		// ---- Anthropic ----
		"claude-3-5-haiku-latest": {
			Name:        "Claude 3.5 Haiku",
			ContextSize: 200000,
			Speed:       "fast",
			Quality:     "good for simple updates",
		},
		"claude-sonnet-4-6": {
			Name:        "Claude Sonnet 4.6",
			ContextSize: 200000,
			Speed:       "balanced",
			Quality:     "excellent for most updates",
		},
		"claude-opus-4-6": {
			Name:        "Claude Opus 4.6",
			ContextSize: 200000,
			Speed:       "moderate",
			Quality:     "best for large/complex changes",
		},
		// ---- Ollama (local) ----
		"qwen3:4b": {
			Name:        "Qwen3 4B",
			ContextSize: 32000,
			Speed:       "fast",
			Quality:     "good for simple updates",
		},
		"qwen3:8b": {
			Name:        "Qwen3 8B",
			ContextSize: 32000,
			Speed:       "balanced",
			Quality:     "good for most updates",
		},
		"qwen3:14b": {
			Name:        "Qwen3 14B",
			ContextSize: 32000,
			Speed:       "moderate",
			Quality:     "excellent for complex updates",
		},
		"qwen3:32b": {
			Name:        "Qwen3 32B",
			ContextSize: 32000,
			Speed:       "slower",
			Quality:     "best for large/complex changes",
		},
		"llama3.1": {
			Name:        "Llama 3.1 8B",
			ContextSize: 128000,
			Speed:       "balanced",
			Quality:     "good for most updates",
		},
		"llama3.1:70b": {
			Name:        "Llama 3.1 70B",
			ContextSize: 128000,
			Speed:       "slow",
			Quality:     "excellent for complex updates",
		},
		"codestral": {
			Name:        "Codestral 22B",
			ContextSize: 32000,
			Speed:       "moderate",
			Quality:     "excellent for code documentation",
		},
	}
}

// ModelInfo contains metadata about a model.
type ModelInfo struct {
	Name        string
	ContextSize int
	Speed       string
	Quality     string
}

// IsModelSuitable checks if a model can handle the given context size.
func IsModelSuitable(model string, requiredContext int) bool {
	models := SupportedModels()
	info, ok := models[model]
	if !ok {
		return true // Unknown model, assume it works
	}
	return info.ContextSize >= requiredContext
}

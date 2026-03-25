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
func (ms *ModelSelector) SelectModel(totalBytes int) ModelRecommendation {
	// If a specific model is configured, use it
	if ms.cfg.Model != "" {
		return ModelRecommendation{
			Model:      ms.cfg.Model,
			Reason:     "explicitly configured",
			Confidence: 1.0,
		}
	}

	// Auto-selection based on diff size
	switch {
	case totalBytes < 1000:
		// Small diff - use fast model
		return ModelRecommendation{
			Model:      "qwen3:4b",
			Reason:     "small diff (<1KB), using fast model",
			Confidence: 0.9,
		}
	case totalBytes < 10000:
		// Medium diff - balanced model
		return ModelRecommendation{
			Model:      "qwen3:8b",
			Reason:     "medium diff (<10KB), using balanced model",
			Confidence: 0.85,
		}
	case totalBytes < 50000:
		// Large diff - more capable model
		return ModelRecommendation{
			Model:      "qwen3:14b",
			Reason:     "large diff (<50KB), using capable model",
			Confidence: 0.8,
		}
	default:
		// Very large diff - most capable model
		return ModelRecommendation{
			Model:      "qwen3:32b",
			Reason:     "very large diff (>=50KB), using most capable model",
			Confidence: 0.75,
		}
	}
}

// SupportedModels returns the list of known models and their characteristics.
func SupportedModels() map[string]ModelInfo {
	return map[string]ModelInfo{
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

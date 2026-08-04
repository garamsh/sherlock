package main

import (
	"fmt"
	"os"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/openai"
)

// newSummarizerFunc resolves the chat-model config into the memory
// Summarizer every environment's memory service shares: the mock
// provider has no model to ask, so it falls back to the deterministic
// concat summarizer; the openai provider builds a chat-completions
// summarizer wrapped in the threshold composite that picks the model
// path only when the cluster's joined length exceeds SummaryMaxLen. A
// missing or empty API key environment variable fails fast at startup
// instead of mid-consolidation.
func newSummarizerFunc(cfg ModelConfig) (memory.Summarizer, error) {
	switch cfg.Provider {
	case "mock":
		return memory.NewConcatSummarizer(), nil
	case "openai":
		key := os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("summarizer provider %q: environment variable %s is not set or is empty",
				cfg.Provider, cfg.APIKeyEnv)
		}
		quality, err := openai.NewSummarizer(openai.Config{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			APIKey:  key,
			Timeout: cfg.Timeout,
		})
		if err != nil {
			return nil, err
		}
		return memory.ThresholdSummarizer{
			Fast:    memory.NewConcatSummarizer(),
			Quality: quality,
		}, nil
	default:
		return nil, fmt.Errorf("unknown summarizer provider %q (want %q or %q)",
			cfg.Provider, "mock", "openai")
	}
}

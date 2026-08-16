package main

import (
	"fmt"
	"os"

	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/openai"
)

// newEmbedderFunc resolves the embedder config into the memory embedder
// the agent's memory service uses: hashing keeps the
// deterministic offline default; openai builds a real client. A missing
// or empty API key environment variable fails fast here, at startup,
// instead of mid-search.
func newEmbedderFunc(cfg config.ModelConfig) (memory.Embedder, error) {
	switch cfg.Provider {
	case "hashing":
		return memory.NewHashingEmbedder(), nil
	case "openai":
		key := os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("embedder provider %q: environment variable %s is not set or is empty",
				cfg.Provider, cfg.APIKeyEnv)
		}
		// One stateless client for the agent's memory service.
		return openai.New(openai.Config{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			APIKey:  key,
			Timeout: cfg.Timeout,
		})
	default:
		return nil, fmt.Errorf("unknown embedder provider %q (want %q or %q)",
			cfg.Provider, "hashing", "openai")
	}
}

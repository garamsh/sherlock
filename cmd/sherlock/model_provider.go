package main

import (
	"fmt"
	"os"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/anthropic"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/config"
)

// newModelFunc resolves the model config into the Factory.NewModel wiring:
// mock keeps the scripted placeholder; openai-compatible and
// anthropic-compatible each build a real client. The value selects the
// wire protocol the endpoint speaks rather than the vendor behind it, so
// one value serves every endpoint speaking it. A missing or empty API key environment variable fails fast here, at
// startup, instead of mid-turn.
func newModelFunc(cfg config.ModelConfig) (func(agentID string) agent.Model, error) {
	switch cfg.Provider {
	case "mock":
		return newMockModel, nil
	case "openai-compatible":
		key := os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("model.provider %q: environment variable %s is not set or is empty",
				cfg.Provider, cfg.APIKeyEnv)
		}
		// One stateless client for the agent's session.
		model, err := openai.New(openai.Config{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			APIKey:  key,
			Timeout: cfg.Timeout,
		})
		if err != nil {
			return nil, err
		}
		return func(string) agent.Model { return model }, nil
	case "anthropic-compatible":
		key := os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("model.provider %q: environment variable %s is not set or is empty",
				cfg.Provider, cfg.APIKeyEnv)
		}
		// One stateless client for the agent's session.
		model, err := anthropic.New(anthropic.Config{
			BaseURL: cfg.BaseURL,
			Model:   cfg.Model,
			APIKey:  key,
			Timeout: cfg.Timeout,
		})
		if err != nil {
			return nil, err
		}
		return func(string) agent.Model { return model }, nil
	case "openai":
		// Renamed rather than aliased. Accepting the old value would keep
		// the claim the rename exists to stop making: that this selects a
		// vendor. The config edit is one word and this names it.
		return nil, fmt.Errorf("model.provider %q names a vendor where it selects a wire protocol: set it to %q, which is the same protocol and the same wiring",
			cfg.Provider, "openai-compatible")
	default:
		return nil, fmt.Errorf("unknown model.provider %q (want %q, %q for the OpenAI chat-completions wire protocol, or %q for Anthropic's Messages wire protocol)",
			cfg.Provider, "mock", "openai-compatible", "anthropic-compatible")
	}
}

// newEmbedder resolves the embedding: section recall ranks mid episodes with
// (ADR 0004). The mock model provider runs offline, so it takes the mock
// embedder and never reads the section. An empty api-key-env sends no key; a
// named variable that is unset or empty fails here, at startup.
func newEmbedder(cfg config.Config) (agent.Embedder, error) {
	if cfg.Model.Provider == "mock" {
		return mockEmbedder{}, nil
	}
	var key string
	if cfg.Embedding.APIKeyEnv != "" {
		if key = os.Getenv(cfg.Embedding.APIKeyEnv); key == "" {
			return nil, fmt.Errorf("embedding: environment variable %s is not set or is empty", cfg.Embedding.APIKeyEnv)
		}
	}
	embedder, err := openai.NewEmbedder(openai.Config{BaseURL: cfg.Embedding.BaseURL, Model: cfg.Embedding.Model, APIKey: key})
	if err != nil {
		return nil, fmt.Errorf("embedding (required with model.provider %q): %w", cfg.Model.Provider, err)
	}
	return embedder, nil
}

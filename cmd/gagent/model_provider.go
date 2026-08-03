package main

import (
	"fmt"
	"os"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/agent/openai"
)

// newModelFunc resolves the model config into the Factory.NewModel wiring:
// mock keeps the scripted placeholder; openai builds a real client. A
// missing or empty API key environment variable fails fast here, at
// startup, instead of mid-turn.
func newModelFunc(cfg ModelConfig) (func(agentID string) agent.Model, error) {
	switch cfg.Provider {
	case "mock":
		return mockModel, nil
	case "openai":
		key := os.Getenv(cfg.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("model provider %q: environment variable %s is not set or is empty",
				cfg.Provider, cfg.APIKeyEnv)
		}
		// One stateless client shared by every environment's session.
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
	default:
		return nil, fmt.Errorf("unknown model provider %q (want %q or %q)",
			cfg.Provider, "mock", "openai")
	}
}

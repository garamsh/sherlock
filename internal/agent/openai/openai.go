package openai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout bounds one chat-completions request when Config.Timeout
// is zero.
const DefaultTimeout = 60 * time.Second

// Config carries the provider settings, already parsed at the command
// boundary. APIKey holds the secret value resolved from the environment —
// never a config-file literal (ADR 0004).
type Config struct {
	// BaseURL is the provider's API root, e.g. https://api.openai.com/v1.
	BaseURL string
	// Model is the model name sent with every request.
	Model string
	// APIKey authenticates requests as a Bearer token.
	APIKey string
	// Timeout bounds one request; zero selects DefaultTimeout.
	Timeout time.Duration
}

// Model is an OpenAI-compatible chat-completions client implementing
// agent.Model. It is stateless across calls and safe for concurrent use.
type Model struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

// New returns a Model for cfg, rejecting incomplete settings so a
// misconfigured provider fails at startup instead of mid-turn.
func New(cfg Config) (*Model, error) {
	var missing []string
	if cfg.BaseURL == "" {
		missing = append(missing, "base URL")
	}
	if cfg.Model == "" {
		missing = append(missing, "model name")
	}
	if cfg.APIKey == "" {
		missing = append(missing, "API key")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("openai provider: missing %s", strings.Join(missing, ", "))
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &Model{
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/chat/completions",
		model:    cfg.Model,
		apiKey:   cfg.APIKey,
		http:     &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// APIError reports a non-2xx response from the provider.
type APIError struct {
	StatusCode int
	Body       string
}

// Error returns a human-readable description of the provider failure.
func (e *APIError) Error() string {
	return fmt.Sprintf("openai provider: HTTP %d: %s", e.StatusCode, e.Body)
}

// errNoChoices marks a provider response without any choice to map.
var errNoChoices = errors.New("openai provider: response has no choices")

package anthropic

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout bounds one messages request when Config.Timeout is zero.
const DefaultTimeout = 60 * time.Second

// DefaultMaxTokens bounds one response when Config.MaxTokens is zero. The
// Messages API requires the field, unlike chat-completions where it is
// optional, so a default is the only way to keep the config surface the
// same across the two protocols.
const DefaultMaxTokens = 4096

// apiVersion is the Messages API version this client is written against.
// The API requires the header on every request and uses it to keep older
// clients on the shape they were built for.
const apiVersion = "2023-06-01"

// Config carries the provider settings, already parsed at the command
// boundary. APIKey holds the secret value resolved from the environment —
// never a config-file literal (ADR 0003); Credential stands in its place
// for a key that is rewritten while the agent runs (ADR 0029).
type Config struct {
	// BaseURL is the API root, e.g. https://api.anthropic.com/v1.
	BaseURL string
	// Model is the model name sent with every request.
	Model string
	// APIKey authenticates requests through the x-api-key header.
	APIKey string
	// Credential, set instead of APIKey, is read for every request, and
	// read once more to retry a request the endpoint answers with 401.
	Credential Credential
	// Timeout bounds one request; zero selects DefaultTimeout.
	Timeout time.Duration
	// MaxTokens bounds one response; zero selects DefaultMaxTokens.
	MaxTokens int
}

// Model is an Anthropic Messages client implementing agent.Model. It is
// stateless across calls and safe for concurrent use.
type Model struct {
	endpoint   string
	model      string
	apiKey     string
	credential Credential
	maxTokens  int
	http       *http.Client
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
	if cfg.APIKey == "" && cfg.Credential == nil {
		missing = append(missing, "API key")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("anthropic provider: missing %s", strings.Join(missing, ", "))
	}
	if cfg.APIKey != "" && cfg.Credential != nil {
		return nil, errors.New("anthropic provider: an API key and a credential are both set; set one")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	return &Model{
		endpoint:   strings.TrimSuffix(cfg.BaseURL, "/") + "/messages",
		model:      cfg.Model,
		apiKey:     cfg.APIKey,
		credential: cfg.Credential,
		maxTokens:  cfg.MaxTokens,
		http:       &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// APIError reports a non-2xx response from the provider.
type APIError struct {
	StatusCode int
	Body       string
}

// Error returns a human-readable description of the provider failure.
func (e *APIError) Error() string {
	return fmt.Sprintf("anthropic provider: HTTP %d: %s", e.StatusCode, e.Body)
}

// errNoContent marks a response carrying no content block to map.
var errNoContent = errors.New("anthropic provider: response has no content blocks")

// ErrResponseTruncated reports a response the model stopped emitting at
// the token ceiling. The reply is a partial document, and the session
// parses Content as a whole document, so returning it would surface a
// truncation as a parse failure somewhere else. Raise MaxTokens.
var ErrResponseTruncated = errors.New("anthropic provider: response hit max_tokens and is incomplete")

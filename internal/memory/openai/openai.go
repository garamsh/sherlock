package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/garamsh/gagent/internal/memory"
)

// DefaultTimeout bounds one request when Config.Timeout is zero. It
// applies to both the embeddings and chat-completions clients wired by
// this package.
const DefaultTimeout = 60 * time.Second

// maxErrorBody caps how much of an error response body is kept in
// APIError. Shared between the embeddings and chat-completions clients.
const maxErrorBody = 512

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

// validateConfig rejects incomplete settings so a misconfigured provider
// fails at startup instead of at first call.
func validateConfig(cfg Config) error {
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
		return fmt.Errorf("openai provider: missing %s", strings.Join(missing, ", "))
	}
	return nil
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

// errNoChoices marks a chat-completions response without any choice to
// map.
var errNoChoices = errors.New("openai provider: response has no choices")

// errNoData marks an embeddings response without any embedding to map.
var errNoData = errors.New("openai embedder: response has no data")

// newHTTPClient returns a client honoring cfg.Timeout.
func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Embedder is an OpenAI-compatible embeddings client implementing
// memory.Embedder. It is stateless across calls and safe for concurrent
// use.
type Embedder struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

var _ memory.Embedder = (*Embedder)(nil)

// New returns an Embedder for cfg, rejecting incomplete settings so a
// misconfigured provider fails at startup instead of mid-search.
func New(cfg Config) (*Embedder, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	return &Embedder{
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/embeddings",
		model:    cfg.Model,
		apiKey:   cfg.APIKey,
		http:     newHTTPClient(cfg.Timeout),
	}, nil
}

// embedRequest is the embeddings request body.
type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// embedResponse is the subset of the embeddings response the service
// consumes: the first entry's vector.
type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed implements memory.Embedder: one embeddings request per text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(snippet)}
	}
	var decoded embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Data) == 0 {
		return nil, errNoData
	}
	return decoded.Data[0].Embedding, nil
}

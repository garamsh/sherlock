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
)

// Embedder is an OpenAI-compatible embeddings client implementing
// agent.Embedder. It is stateless across calls and safe for concurrent use.
type Embedder struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

// NewEmbedder returns an Embedder for cfg. An empty APIKey sends no
// Authorization header, which a local server needs none of.
func NewEmbedder(cfg Config) (*Embedder, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, errors.New("openai embeddings: base URL and model name are required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &Embedder{
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/embeddings",
		model:    cfg.Model,
		apiKey:   cfg.APIKey,
		http:     &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// Embed returns the vector the endpoint assigns to text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{e.model, []string{text}})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(snippet)}
	}
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Data) != 1 || len(decoded.Data[0].Embedding) == 0 {
		return nil, errors.New("openai embeddings: response must carry one non-empty embedding")
	}
	return decoded.Data[0].Embedding, nil
}

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

	"github.com/garamsh/gagent/internal/memory"
)

// summarizerSystemPrompt tells the chat model to compress a cluster of
// episodic memories into a single coherent summary the service can
// chunk into searchable parents.
const summarizerSystemPrompt = "You compress a cluster of episodic memories into one coherent summary. " +
	"Preserve every distinct fact and instruction verbatim — the summary is the only searchable " +
	"index for the cluster, so any fact left out becomes a silent memory loss. " +
	"Return the summary as plain prose, no bullet list, no commentary."

// Summarizer is an OpenAI-compatible chat-completions-backed
// memory.Summarizer. It is stateless across calls and safe for
// concurrent use. It is constructed in cmd/gagent/summarizer_provider.go
// from the chat model's config section — the same values that build the
// agent's model — through this package's Config, which it shares with
// the memory Embedder rather than with internal/agent/openai.
type Summarizer struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

var _ memory.Summarizer = (*Summarizer)(nil)

// summarizerRequest is the wire shape for one chat-completions call.
type summarizerRequest struct {
	Model    string              `json:"model"`
	Messages []summarizerMessage `json:"messages"`
}

// summarizerMessage is one chat-completions entry: the system prompt and
// the contents joined as a single user turn.
type summarizerMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// summarizerResponse mirrors the subset of the chat-completions reply
// the summarizer reads: the first choice's content.
type summarizerResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// NewSummarizer returns a Summarizer for cfg. It rejects incomplete
// settings so a misconfigured chat model fails at startup instead of at
// shutdown consolidation.
func NewSummarizer(cfg Config) (*Summarizer, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &Summarizer{
		endpoint: strings.TrimSuffix(cfg.BaseURL, "/") + "/chat/completions",
		model:    cfg.Model,
		apiKey:   cfg.APIKey,
		http:     &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// Summarize implements memory.Summarizer: one chat-completions request
// with the contents joined as the user turn. The result reports
// Generated true — a model wrote it. Errors propagate to the caller —
// the threshold composite falls back to the concat summarizer when the
// model is unavailable, and that fallback reports Generated false.
func (s *Summarizer) Summarize(ctx context.Context, contents []string) (memory.Summary, error) {
	if len(contents) == 0 {
		return memory.Summary{}, fmt.Errorf("summarize: %w: no contents", memory.ErrInvalidInput)
	}
	body, err := json.Marshal(summarizerRequest{
		Model: s.model,
		Messages: []summarizerMessage{
			{Role: "system", Content: summarizerSystemPrompt},
			{Role: "user", Content: strings.Join(contents, "\n\n")},
		},
	})
	if err != nil {
		return memory.Summary{}, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return memory.Summary{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.http.Do(req)
	if err != nil {
		return memory.Summary{}, fmt.Errorf("chat completions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return memory.Summary{}, &APIError{StatusCode: resp.StatusCode, Body: string(snippet)}
	}
	var decoded summarizerResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return memory.Summary{}, fmt.Errorf("decode response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return memory.Summary{}, errNoChoices
	}
	out := decoded.Choices[0].Message.Content
	if out == "" {
		return memory.Summary{}, errors.New("openai summarizer: empty response content")
	}
	return memory.Summary{Text: out, Generated: true}, nil
}

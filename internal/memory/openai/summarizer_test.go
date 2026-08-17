package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/openai"
)

// summarizerRequest mirrors the wire shape for one summarization call.
type summarizerRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// summarizerProvider records each call and replies with a handler-chosen
// body at the configured HTTP status.
type summarizerProvider struct {
	t       *testing.T
	auth    string
	model   string
	status  int
	reply   string
	content string
	calls   atomic.Int32
}

func (p *summarizerProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	p.calls.Add(1)
	require.Equal(p.t, http.MethodPost, r.Method)
	require.Equal(p.t, "/v1/chat/completions", r.URL.Path)
	p.auth = r.Header.Get("Authorization")
	var body summarizerRequest
	require.NoError(p.t, json.NewDecoder(r.Body).Decode(&body))
	p.model = body.Model
	require.NotEmpty(p.t, body.Messages, "the summarizer sends a system+user turn")
	assert.Equal(p.t, "system", body.Messages[0].Role,
		"the summarizer's first message is the system prompt")
	assert.Equal(p.t, "user", body.Messages[1].Role,
		"the cluster contents go in as a single user turn")
	p.content = body.Messages[1].Content

	w.Header().Set("Content-Type", "application/json")
	if p.status != 0 {
		w.WriteHeader(p.status)
	}
	_, _ = w.Write([]byte(p.reply))
}

func newSummarizerServer(t *testing.T, reply string) (*summarizerProvider, *httptest.Server) {
	t.Helper()
	p := &summarizerProvider{t: t, reply: reply}
	return p, httptest.NewServer(http.HandlerFunc(p.serveHTTP))
}

func newTestSummarizer(t *testing.T, server *httptest.Server) *openai.Summarizer {
	t.Helper()
	s, err := openai.NewSummarizer(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "test-model",
		APIKey:  "sk-test",
	})
	require.NoError(t, err)
	return s
}

func TestSummarize_RoundTrip(t *testing.T) {
	provider, server := newSummarizerServer(t,
		`{"choices":[{"message":{"content":"a coherent summary of all facts"}}]}`)
	defer server.Close()
	s := newTestSummarizer(t, server)

	out, err := s.Summarize(context.Background(),
		[]string{"the cat sat", "the dog ran", "the bird flew"})
	require.NoError(t, err)
	assert.Equal(t, "a coherent summary of all facts", out.Text,
		"the summarizer returns the model's first choice content")
	assert.True(t, out.Generated,
		"a model wrote it, which is what lets the caller store it (ADR 0004, ADR 0006)")
	assert.Equal(t, "Bearer sk-test", provider.auth,
		"the API key authenticates as a Bearer token")
	assert.Equal(t, "test-model", provider.model)
	assert.Equal(t, 1, int(provider.calls.Load()))
	assert.Contains(t, provider.content, "the cat sat",
		"every cluster content reaches the model verbatim")
	assert.Contains(t, provider.content, "the bird flew",
		"no content is filtered before the model sees it")
}

func TestSummarize_NoContentsFails(t *testing.T) {
	_, server := newSummarizerServer(t, "")
	defer server.Close()
	s := newTestSummarizer(t, server)

	_, err := s.Summarize(context.Background(), nil)
	require.ErrorIs(t, err, memory.ErrInvalidInput)
}

func TestSummarize_EmptyChoiceFails(t *testing.T) {
	_, server := newSummarizerServer(t, `{"choices":[]}`)
	defer server.Close()
	s := newTestSummarizer(t, server)

	_, err := s.Summarize(context.Background(), []string{"x"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, memory.ErrInvalidInput,
		"an empty-choice response is a provider failure, not an input error")
}

func TestSummarize_EmptyContentFails(t *testing.T) {
	_, server := newSummarizerServer(t,
		`{"choices":[{"message":{"content":""}}]}`)
	defer server.Close()
	s := newTestSummarizer(t, server)

	_, err := s.Summarize(context.Background(), []string{"x"})
	require.Error(t, err)
}

func TestSummarize_Non2xxReturnsTypedError(t *testing.T) {
	provider, server := newSummarizerServer(t, `{"error":{"message":"bad key"}}`)
	provider.status = http.StatusUnauthorized
	defer server.Close()
	s := newTestSummarizer(t, server)

	_, err := s.Summarize(context.Background(), []string{"x"})
	var apiErr *openai.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "bad key")
}

func TestSummarize_ConfiguredTimeoutBoundsRequest(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-t.Context().Done()
	}))
	defer slow.Close()

	s, err := openai.NewSummarizer(openai.Config{
		BaseURL: slow.URL + "/v1",
		Model:   "test-model",
		APIKey:  "sk-test",
		Timeout: time.Nanosecond,
	})
	require.NoError(t, err)

	_, err = s.Summarize(context.Background(), []string{"x"})
	require.Error(t, err)
	var apiErr *openai.APIError
	assert.False(t, errors.As(err, &apiErr), "a timeout is a transport error, not an API error")
}

func TestSummarizer_New_MissingSettingsFailFast(t *testing.T) {
	tests := []struct {
		name string
		cfg  openai.Config
	}{
		{"no base URL", openai.Config{Model: "m", APIKey: "k"}},
		{"no model", openai.Config{BaseURL: "http://x", APIKey: "k"}},
		{"no API key", openai.Config{BaseURL: "http://x", Model: "m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openai.NewSummarizer(tt.cfg)
			require.Error(t, err)
		})
	}
}

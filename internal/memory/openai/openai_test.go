package openai_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory/openai"
)

// fakeProvider records the last request and replies with a
// handler-chosen body at the configured HTTP status.
type fakeProvider struct {
	t      *testing.T
	auth   string
	model  string
	input  string
	status int
	reply  string
}

func (f *fakeProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	require.Equal(f.t, http.MethodPost, r.Method)
	require.Equal(f.t, "/v1/embeddings", r.URL.Path)
	f.auth = r.Header.Get("Authorization")
	var body struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.model = body.Model
	f.input = body.Input
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_, _ = w.Write([]byte(f.reply))
}

func newFakeProvider(t *testing.T, reply string) (*fakeProvider, *httptest.Server) {
	t.Helper()
	f := &fakeProvider{t: t, reply: reply}
	return f, httptest.NewServer(http.HandlerFunc(f.serveHTTP))
}

func newTestEmbedder(t *testing.T, server *httptest.Server) *openai.Embedder {
	t.Helper()
	embedder, err := openai.New(openai.Config{
		BaseURL: server.URL + "/v1",
		Model:   "test-embedding",
		APIKey:  "sk-test",
	})
	require.NoError(t, err)
	return embedder
}

func TestEmbed_RoundTrip(t *testing.T) {
	fake, server := newFakeProvider(t, `{"data":[{"embedding":[0.5,-1.25,2]}]}`)
	defer server.Close()

	vec, err := newTestEmbedder(t, server).Embed(t.Context(), "remember this")
	require.NoError(t, err)

	assert.Equal(t, []float32{0.5, -1.25, 2}, vec)
	assert.Equal(t, "Bearer sk-test", fake.auth, "the API key authenticates as a Bearer token")
	assert.Equal(t, "test-embedding", fake.model)
	assert.Equal(t, "remember this", fake.input)
}

func TestEmbed_Non2xxReturnsTypedError(t *testing.T) {
	fake, server := newFakeProvider(t, `{"error":{"message":"bad key"}}`)
	fake.status = http.StatusUnauthorized
	defer server.Close()

	_, err := newTestEmbedder(t, server).Embed(t.Context(), "text")
	var apiErr *openai.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "bad key")
}

func TestEmbed_MalformedResponseFails(t *testing.T) {
	_, server := newFakeProvider(t, `not json`)
	defer server.Close()

	_, err := newTestEmbedder(t, server).Embed(t.Context(), "text")
	assert.Error(t, err)
}

func TestEmbed_NoDataFails(t *testing.T) {
	_, server := newFakeProvider(t, `{"data":[]}`)
	defer server.Close()

	_, err := newTestEmbedder(t, server).Embed(t.Context(), "text")
	assert.Error(t, err)
}

func TestNew_MissingSettingsFailFast(t *testing.T) {
	tests := []struct {
		name string
		cfg  openai.Config
	}{
		{"base URL", openai.Config{Model: "m", APIKey: "k"}},
		{"model name", openai.Config{BaseURL: "http://x", APIKey: "k"}},
		{"API key", openai.Config{BaseURL: "http://x", Model: "m"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openai.New(tt.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.name)
		})
	}
}

func TestEmbed_ConfiguredTimeoutBoundsRequest(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-t.Context().Done()
	}))
	defer slow.Close()

	embedder, err := openai.New(openai.Config{
		BaseURL: slow.URL + "/v1",
		Model:   "test-embedding",
		APIKey:  "sk-test",
		Timeout: time.Nanosecond,
	})
	require.NoError(t, err)

	_, err = embedder.Embed(t.Context(), "text")
	require.Error(t, err)
	var apiErr *openai.APIError
	assert.False(t, errors.As(err, &apiErr), "a timeout is a transport error, not an API error")
}

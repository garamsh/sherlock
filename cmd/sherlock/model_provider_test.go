package main_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modelEndpoint is a local stand-in for a model provider: it answers the
// embeddings route with a fixed vector, every other route after delay
// with 400, and records each request's path, credential headers and turn
// header. No
// request leaves the host.
type modelEndpoint struct {
	URL string

	mu       sync.Mutex
	requests []endpointRequest
}

// endpointRequest is what modelEndpoint recorded of one request.
type endpointRequest struct {
	Path, Authorization, APIKey, TurnID string
}

func newModelEndpoint(t *testing.T, delay time.Duration) *modelEndpoint {
	t.Helper()
	e := &modelEndpoint{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.requests = append(e.requests, endpointRequest{
			Path: r.URL.Path, Authorization: r.Header.Get("Authorization"), APIKey: r.Header.Get("x-api-key"),
			TurnID: r.Header.Get("X-Turn-Id"),
		})
		e.mu.Unlock()
		if r.URL.Path == "/v1/embeddings" {
			_, _ = w.Write([]byte(`{"data":[{"embedding":[1]}]}`))
			return
		}
		time.Sleep(delay)
		http.Error(w, `{"error":{"message":"refused by the test endpoint"}}`, http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/v1"
	return e
}

// request returns the first request recorded on path, and whether there
// was one.
func (e *modelEndpoint) request(path string) (endpointRequest, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.requests {
		if r.Path == path {
			return r, true
		}
	}
	return endpointRequest{}, false
}

// providerTurn runs one turn over a tree holding only message_send's
// manifest, under a config whose model: and embedding: sections are the
// given YAML, and returns the turn's error.
func providerTurn(t *testing.T, model, embedding string) error {
	t.Helper()
	dir, _ := pinnedToolTree(t)
	configPath := writeConfig(t, fmt.Sprintf("tools-dir: %q\nmodel:\n%sembedding:\n%s", dir, model, embedding))
	return runTurn(configPath, filepath.Join(t.TempDir(), "memory.db"))
}

// liveEmbedding is an embedding: section naming e's embeddings route.
func liveEmbedding(e *modelEndpoint) string {
	return fmt.Sprintf("  base-url: %q\n  model: test-embedder\n", e.URL)
}

func TestAgentCommand_MockProviderRunsTheScriptedModel(t *testing.T) {
	assert.EqualError(t, providerTurn(t, "  provider: mock\n", ""), mockTurnRan,
		"the turn runs the scripted scenario")
}

func TestAgentCommand_UnknownProviderFails(t *testing.T) {
	err := providerTurn(t, "  provider: anthropic\n", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown model.provider")
}

// TestAgentCommand_RetiredOpenAIProviderNamesItsReplacement pins the
// rename (#613): "openai" named a vendor where the value selects a wire
// protocol. It is refused rather than aliased, so the operator's config
// stops making the claim — which is only defensible if the refusal says
// what to write instead. The assertion is on the replacement being named,
// not merely on an error, because an error that does not name it breaks a
// working config without repairing it.
func TestAgentCommand_RetiredOpenAIProviderNamesItsReplacement(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	err := providerTurn(t, "  provider: openai\n  base-url: http://localhost:1234/v1\n"+
		"  model: test-model\n  api-key-env: SHERLOCK_TEST_KEY\n", "")
	require.Error(t, err, "the retired value must not silently keep working")
	assert.Contains(t, err.Error(), "openai-compatible",
		"the refusal must name the replacement the operator has to write")
}

// TestAgentCommand_AnthropicCompatibleProviderSpeaksMessages pins the
// second wire protocol reaching its own package (#621): selecting it must
// not fall through to the OpenAI-compatible client, which cannot speak it.
func TestAgentCommand_AnthropicCompatibleProviderSpeaksMessages(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "test-key")
	endpoint := newModelEndpoint(t, 0)
	err := providerTurn(t, fmt.Sprintf("  provider: anthropic-compatible\n  base-url: %q\n"+
		"  model: claude-test\n  api-key-env: SHERLOCK_TEST_KEY\n", endpoint.URL), liveEmbedding(endpoint))
	require.Error(t, err, "the test endpoint refuses the model call")

	req, ok := endpoint.request("/v1/messages")
	require.True(t, ok, "the turn called the Messages route")
	assert.Equal(t, "test-key", req.APIKey)
	_, ok = endpoint.request("/v1/chat/completions")
	assert.False(t, ok, "the turn never called the chat-completions route")
}

// TestAgentCommand_AnthropicCompatibleMissingKeyEnvFailsFast pins that
// the new protocol fails at startup on a missing key exactly as the
// other one does, rather than on the first turn.
func TestAgentCommand_AnthropicCompatibleMissingKeyEnvFailsFast(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_UNSET_KEY", "")
	err := providerTurn(t, "  provider: anthropic-compatible\n  base-url: https://api.anthropic.com/v1\n"+
		"  model: claude-test\n  api-key-env: SHERLOCK_TEST_UNSET_KEY\n", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHERLOCK_TEST_UNSET_KEY")
}

func TestAgentCommand_OpenAICompatibleMissingKeyEnvFailsFast(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_UNSET_KEY", "")
	err := providerTurn(t, "  provider: openai-compatible\n  base-url: http://localhost:1234/v1\n"+
		"  model: test-model\n  api-key-env: SHERLOCK_TEST_UNSET_KEY\n", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHERLOCK_TEST_UNSET_KEY")
}

func TestAgentCommand_OpenAICompatibleProviderSpeaksChatCompletions(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	endpoint := newModelEndpoint(t, 0)
	err := providerTurn(t, fmt.Sprintf("  provider: openai-compatible\n  base-url: %q\n"+
		"  model: test-model\n  api-key-env: SHERLOCK_TEST_KEY\n", endpoint.URL), liveEmbedding(endpoint))
	require.Error(t, err, "the test endpoint refuses the model call")

	req, ok := endpoint.request("/v1/chat/completions")
	require.True(t, ok, "the turn called the chat-completions route")
	assert.Equal(t, "Bearer sk-test", req.Authorization)
	_, ok = endpoint.request("/v1/messages")
	assert.False(t, ok, "the turn never called the Messages route")
}

func TestAgentCommand_OpenAICompatibleIncompleteSettingsFail(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	err := providerTurn(t, "  provider: openai-compatible\n  base-url: \"\"\n"+
		"  model: test-model\n  api-key-env: SHERLOCK_TEST_KEY\n", liveEmbedding(newModelEndpoint(t, 0)))
	require.Error(t, err, "empty base URL is rejected at startup")
	assert.Contains(t, err.Error(), "openai provider: missing base URL")
}

// TestAgentCommand_OpenAICompatibleConfiguredTimeoutReachesModel pins the
// wiring gap that issue #70 describes: model.timeout must reach the
// timeout that bounds the model client's requests. A slow endpoint plus a
// short timeout forces the turn to fail in well under the endpoint's
// delay, which can only happen when the timeout is honored.
func TestAgentCommand_OpenAICompatibleConfiguredTimeoutReachesModel(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	endpoint := newModelEndpoint(t, 2*time.Second)

	start := time.Now()
	err := providerTurn(t, fmt.Sprintf("  provider: openai-compatible\n  base-url: %q\n"+
		"  model: test-model\n  api-key-env: SHERLOCK_TEST_KEY\n  timeout: 50ms\n", endpoint.URL),
		liveEmbedding(endpoint))
	elapsed := time.Since(start)
	require.Error(t, err, "the slow endpoint must time out")
	_, ok := endpoint.request("/v1/chat/completions")
	require.True(t, ok, "the turn called the model")
	assert.Less(t, elapsed, time.Second,
		"the configured timeout must bound the request; elapsed=%s", elapsed)
}

// TestAgentCommand_EmbeddingSection holds how the embedding: section
// resolves (ADR 0004): the mock model never reads it, a live model needs
// its endpoint, and a key variable the section names must be set.
func TestAgentCommand_EmbeddingSection(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	t.Setenv("SHERLOCK_TEST_EMBED_KEY", "")
	endpoint := newModelEndpoint(t, 0)
	live := fmt.Sprintf("  provider: openai-compatible\n  base-url: %q\n"+
		"  model: test-model\n  api-key-env: SHERLOCK_TEST_KEY\n", endpoint.URL)

	t.Run("mock model runs offline without the section", func(t *testing.T) {
		assert.EqualError(t, providerTurn(t, "  provider: mock\n", ""), mockTurnRan)
	})
	t.Run("live model with an endpoint and no key", func(t *testing.T) {
		require.Error(t, providerTurn(t, live, liveEmbedding(endpoint)), "the test endpoint refuses the model call")
		_, ok := endpoint.request("/v1/chat/completions")
		assert.True(t, ok, "startup accepted the section and the turn reached the model")
	})
	t.Run("live model without the section", func(t *testing.T) {
		assert.ErrorContains(t, providerTurn(t, live, ""), "base URL and model name are required")
	})
	t.Run("named key variable unset", func(t *testing.T) {
		assert.ErrorContains(t, providerTurn(t, live, liveEmbedding(endpoint)+"  api-key-env: SHERLOCK_TEST_EMBED_KEY\n"),
			"SHERLOCK_TEST_EMBED_KEY is not set or is empty")
	})
}

// TestAgentCommand_KeyFileAndTurnIDReachTheModel runs a turn configured
// with model.api-key-file (ADR 0029): the model request carries the key
// the file holds, not the variable api-key-env defaults to, and the
// turn's identifier in its header.
func TestAgentCommand_KeyFileAndTurnIDReachTheModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "from-the-environment")
	keyPath := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(keyPath, []byte("from-the-file\n"), 0o600))
	endpoint := newModelEndpoint(t, 0)
	err := providerTurn(t, fmt.Sprintf("  provider: openai-compatible\n  base-url: %q\n"+
		"  model: auto\n  api-key-file: %q\n", endpoint.URL, keyPath), liveEmbedding(endpoint))
	require.Error(t, err, "the test endpoint refuses the model call")

	req, ok := endpoint.request("/v1/chat/completions")
	require.True(t, ok, "the turn called the chat-completions route")
	assert.Equal(t, "Bearer from-the-file", req.Authorization)
	assert.Regexp(t, "^[0-9a-f]{32}$", req.TurnID, "the request carries the turn's identifier")
}

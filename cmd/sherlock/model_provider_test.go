package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/config"
)

func TestNewModelFunc_MockKeepsScriptedModel(t *testing.T) {
	newModel, err := newModelFunc(config.ModelConfig{Provider: "mock"})
	require.NoError(t, err)
	assert.NotNil(t, newModel("any-env"))
}

func TestNewModelFunc_UnknownProviderFails(t *testing.T) {
	_, err := newModelFunc(config.ModelConfig{Provider: "anthropic"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown model.provider")
}

// TestNewModelFunc_RetiredOpenAIValueNamesItsReplacement pins the rename
// (#613): "openai" named a vendor where the value selects a wire protocol.
// It is refused rather than aliased, so the operator's config stops making
// the claim — which is only defensible if the refusal says what to write
// instead. The assertion is on the replacement being named, not merely on
// an error, because an error that does not name it breaks a working config
// without repairing it.
func TestNewModelFunc_RetiredOpenAIValueNamesItsReplacement(t *testing.T) {
	_, err := newModelFunc(config.ModelConfig{
		Provider:  "openai",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-model",
		APIKeyEnv: "SHERLOCK_TEST_KEY",
	})
	require.Error(t, err, "the retired value must not silently keep working")
	assert.Contains(t, err.Error(), "openai-compatible",
		"the refusal must name the replacement the operator has to write")
}

// TestNewModelFunc_AnthropicCompatibleBuildsClient pins the second wire
// protocol reaching its own package (#621): selecting it must not fall
// through to the OpenAI-compatible client, which cannot speak it.
func TestNewModelFunc_AnthropicCompatibleBuildsClient(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "test-key")
	newModel, err := newModelFunc(config.ModelConfig{
		Provider:  "anthropic-compatible",
		BaseURL:   "https://api.anthropic.com/v1",
		Model:     "claude-test",
		APIKeyEnv: "SHERLOCK_TEST_KEY",
	})
	require.NoError(t, err)
	assert.NotNil(t, newModel("any-env"))
}

// TestNewModelFunc_AnthropicCompatibleMissingKeyEnvFailsFast pins that
// the new protocol fails at startup on a missing key exactly as the
// other one does, rather than on the first turn.
func TestNewModelFunc_AnthropicCompatibleMissingKeyEnvFailsFast(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_UNSET_KEY", "")
	_, err := newModelFunc(config.ModelConfig{
		Provider:  "anthropic-compatible",
		BaseURL:   "https://api.anthropic.com/v1",
		Model:     "claude-test",
		APIKeyEnv: "SHERLOCK_TEST_UNSET_KEY",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHERLOCK_TEST_UNSET_KEY")
}

func TestNewModelFunc_OpenAICompatibleMissingKeyEnvFailsFast(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_UNSET_KEY", "")
	_, err := newModelFunc(config.ModelConfig{
		Provider:  "openai-compatible",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-model",
		APIKeyEnv: "SHERLOCK_TEST_UNSET_KEY",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHERLOCK_TEST_UNSET_KEY")
}

func TestNewModelFunc_OpenAICompatibleBuildsClient(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	newModel, err := newModelFunc(config.ModelConfig{
		Provider:  "openai-compatible",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-model",
		APIKeyEnv: "SHERLOCK_TEST_KEY",
	})
	require.NoError(t, err)
	assert.NotNil(t, newModel("any-env"))
}

func TestNewModelFunc_OpenAICompatibleIncompleteSettingsFail(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	_, err := newModelFunc(config.ModelConfig{
		Provider:  "openai-compatible",
		Model:     "test-model",
		APIKeyEnv: "SHERLOCK_TEST_KEY",
	})
	assert.Error(t, err, "empty base URL is rejected at startup")
}

// TestNewModelFunc_OpenAICompatibleConfiguredTimeoutReachesModel pins the wiring
// gap that issue #70 describes: ModelConfig.Timeout must reach the
// openai.Config.Timeout that bounds the underlying http.Client. A slow
// handler plus a short timeout forces the request to fail in well under
// the handler's sleep, which can only happen when the timeout is honored.
func TestNewModelFunc_OpenAICompatibleConfiguredTimeoutReachesModel(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"late"},"finish_reason":"stop"}]}`))
	}))
	defer slow.Close()

	t.Setenv("SHERLOCK_TEST_KEY", "sk-test")
	newModel, err := newModelFunc(config.ModelConfig{
		Provider:  "openai-compatible",
		BaseURL:   slow.URL + "/v1",
		Model:     "test-model",
		APIKeyEnv: "SHERLOCK_TEST_KEY",
		Timeout:   50 * time.Millisecond,
	})
	require.NoError(t, err)
	model := newModel("any-env")

	start := time.Now()
	_, err = model.Chat(t.Context(), []agent.Message{
		{Role: agent.RoleUser, Content: "hi"},
	}, nil)
	elapsed := time.Since(start)
	require.Error(t, err, "the slow handler must time out")
	assert.Less(t, elapsed, time.Second,
		"the configured timeout must bound the request; elapsed=%s", elapsed)
}

func TestNewEmbedder(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_EMBED_KEY", "")
	endpoint := config.EmbeddingConfig{BaseURL: "http://127.0.0.1:8090/v1", Model: "BAAI/bge-base-en-v1.5"}
	for _, tc := range []struct {
		name    string
		cfg     config.Config
		wantErr string
	}{
		{name: "mock model runs offline without the section", cfg: config.Config{Model: config.ModelConfig{Provider: "mock"}}},
		{name: "live model with an endpoint and no key", cfg: config.Config{Model: config.ModelConfig{Provider: "openai-compatible"}, Embedding: endpoint}},
		{name: "live model without the section", cfg: config.Config{Model: config.ModelConfig{Provider: "openai-compatible"}},
			wantErr: "base URL and model name are required"},
		{name: "named key variable unset", cfg: config.Config{Model: config.ModelConfig{Provider: "anthropic-compatible"},
			Embedding: config.EmbeddingConfig{BaseURL: endpoint.BaseURL, Model: endpoint.Model, APIKeyEnv: "SHERLOCK_TEST_EMBED_KEY"}},
			wantErr: "SHERLOCK_TEST_EMBED_KEY is not set or is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			embedder, err := newEmbedder(tc.cfg)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, embedder)
		})
	}
}

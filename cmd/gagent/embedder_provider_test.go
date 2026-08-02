package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/openai"
)

func TestNewEmbedderFunc_HashingKeepsOfflineDefault(t *testing.T) {
	embedder, err := newEmbedderFunc(ModelConfig{Provider: "hashing"})
	require.NoError(t, err)
	assert.Equal(t, memory.NewHashingEmbedder(), embedder)
}

func TestNewEmbedderFunc_UnknownProviderFails(t *testing.T) {
	_, err := newEmbedderFunc(ModelConfig{Provider: "anthropic"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown embedder provider")
}

func TestNewEmbedderFunc_OpenAIMissingKeyEnvFailsFast(t *testing.T) {
	t.Setenv("GAGENT_TEST_UNSET_KEY", "")
	_, err := newEmbedderFunc(ModelConfig{
		Provider:  "openai",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-embedding",
		APIKeyEnv: "GAGENT_TEST_UNSET_KEY",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GAGENT_TEST_UNSET_KEY")
}

func TestNewEmbedderFunc_OpenAIBuildsClient(t *testing.T) {
	t.Setenv("GAGENT_TEST_KEY", "sk-test")
	embedder, err := newEmbedderFunc(ModelConfig{
		Provider:  "openai",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-embedding",
		APIKeyEnv: "GAGENT_TEST_KEY",
	})
	require.NoError(t, err)
	assert.IsType(t, &openai.Embedder{}, embedder)
}

func TestNewEmbedderFunc_OpenAIIncompleteSettingsFail(t *testing.T) {
	t.Setenv("GAGENT_TEST_KEY", "sk-test")
	_, err := newEmbedderFunc(ModelConfig{
		Provider:  "openai",
		Model:     "test-embedding",
		APIKeyEnv: "GAGENT_TEST_KEY",
	})
	assert.Error(t, err, "empty base URL is rejected at startup")
}

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModelFunc_MockKeepsScriptedModel(t *testing.T) {
	newModel, err := newModelFunc(ModelConfig{Provider: "mock"})
	require.NoError(t, err)
	assert.NotNil(t, newModel("any-env"))
}

func TestNewModelFunc_UnknownProviderFails(t *testing.T) {
	_, err := newModelFunc(ModelConfig{Provider: "anthropic"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown model provider")
}

func TestNewModelFunc_OpenAIMissingKeyEnvFailsFast(t *testing.T) {
	t.Setenv("GAGENT_TEST_UNSET_KEY", "")
	_, err := newModelFunc(ModelConfig{
		Provider:  "openai",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-model",
		APIKeyEnv: "GAGENT_TEST_UNSET_KEY",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GAGENT_TEST_UNSET_KEY")
}

func TestNewModelFunc_OpenAIBuildsClient(t *testing.T) {
	t.Setenv("GAGENT_TEST_KEY", "sk-test")
	newModel, err := newModelFunc(ModelConfig{
		Provider:  "openai",
		BaseURL:   "http://localhost:1234/v1",
		Model:     "test-model",
		APIKeyEnv: "GAGENT_TEST_KEY",
	})
	require.NoError(t, err)
	assert.NotNil(t, newModel("any-env"))
}

func TestNewModelFunc_OpenAIIncompleteSettingsFail(t *testing.T) {
	t.Setenv("GAGENT_TEST_KEY", "sk-test")
	_, err := newModelFunc(ModelConfig{
		Provider:  "openai",
		Model:     "test-model",
		APIKeyEnv: "GAGENT_TEST_KEY",
	})
	assert.Error(t, err, "empty base URL is rejected at startup")
}

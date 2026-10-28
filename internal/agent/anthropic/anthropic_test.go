package anthropic_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/anthropic"
)

// TestNew_IncompleteSettingsFailAtStartup pins that a misconfigured
// provider is refused when the process starts rather than on the first
// turn, which is where the operator is still watching.
func TestNew_IncompleteSettingsFailAtStartup(t *testing.T) {
	for name, cfg := range map[string]anthropic.Config{
		"no base URL": {Model: "claude-test", APIKey: "k"},
		"no model":    {BaseURL: "https://api.anthropic.com/v1", APIKey: "k"},
		"no API key":  {BaseURL: "https://api.anthropic.com/v1", Model: "claude-test"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := anthropic.New(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing")
		})
	}
}

// TestNew_CompleteSettingsSatisfyTheSeam is the compile-time half of the
// issue's constraint that agent.Model is not widened: the assignment
// fails to build if this protocol needed a different method.
func TestNew_CompleteSettingsSatisfyTheSeam(t *testing.T) {
	m, err := anthropic.New(anthropic.Config{
		BaseURL:   "https://api.anthropic.com/v1",
		Model:     "claude-test",
		APIKey:    "k",
		Timeout:   5 * time.Second,
		MaxTokens: 128,
	})
	require.NoError(t, err)

	var model agent.Model = m
	assert.NotNil(t, model)
}

func TestNew_APIKeyAndCredentialAreExclusive(t *testing.T) {
	_, err := anthropic.New(anthropic.Config{
		BaseURL: "https://api.anthropic.com/v1", Model: "claude-test", APIKey: "k",
		Credential: &rotatingCredential{keys: []string{"k"}},
	})
	assert.ErrorContains(t, err, "both set")
}

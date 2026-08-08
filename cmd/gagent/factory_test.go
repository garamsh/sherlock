package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
)

// newStubModel returns a no-op model function suitable for tests that only
// exercise the factory wiring: nothing runs the model.
func newStubModel() func(string) agent.Model {
	return func(string) agent.Model { return nil }
}

func TestNewFactory_CarriesResolvedConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	factory, err := newFactory(cfg, newStubModel(), memory.NewHashingEmbedder(), memory.NewConcatSummarizer(), "", nil, nil)
	require.NoError(t, err)

	assert.Equal(t, cfg.MemoryPath, factory.MemoryPath,
		"the resolved memory path must reach the runtime factory")
	assert.NotNil(t, factory.NewModel, "the factory must carry the model builder")
	assert.NotNil(t, factory.Embedder,
		"the resolved embedder must reach the runtime factory")
	assert.NotNil(t, factory.Summarizer,
		"the resolved summarizer must reach the runtime factory")
}

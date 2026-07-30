package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
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

	factory := newFactory(cfg, newStubModel(), "", nil)

	assert.Equal(t, cfg.DataRoot, factory.Root,
		"the resolved data root must reach the environment factory")
	assert.NotNil(t, factory.NewModel, "the factory must carry the model builder")
	assert.Equal(t, cfg.ExecTimeout, factory.ShellExecTimeout,
		"the resolved exec-timeout must reach the environment factory")
	assert.NotNil(t, factory.NewShellExecutor,
		"the factory must carry a shell-exec executor closure")
}

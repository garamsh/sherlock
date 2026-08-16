package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/config"
)

// egoFlags returns a flag set carrying --ego-file, which the agent's
// flag registration is what puts there.
func egoFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	config.AddAgentFlags(fs)
	return fs
}

func TestResolveEgo_NoFlagSelectsEmbeddedDefault(t *testing.T) {
	ego, err := config.ResolveEgo(egoFlags())
	require.NoError(t, err)

	assert.Empty(t, ego, "empty flag defers to the agent package's embedded ego")
}

func TestResolveEgo_ReadsPromptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "EGO.md")
	require.NoError(t, os.WriteFile(path, []byte("You are a test ego.\n"), 0o644))
	fs := egoFlags()
	require.NoError(t, fs.Set("ego-file", path))

	ego, err := config.ResolveEgo(fs)
	require.NoError(t, err)

	assert.Equal(t, "You are a test ego.\n", ego)
}

func TestResolveEgo_MissingFileFailsAtStartup(t *testing.T) {
	fs := egoFlags()
	require.NoError(t, fs.Set("ego-file", filepath.Join(t.TempDir(), "nope.md")))

	_, err := config.ResolveEgo(fs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ego file")
}

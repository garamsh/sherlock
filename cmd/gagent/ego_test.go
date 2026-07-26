package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEgoCommand returns a bare command carrying the --ego-file flag.
func newEgoCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("ego-file", "", "")
	return cmd
}

func TestResolveEgo_NoFlagSelectsEmbeddedDefault(t *testing.T) {
	ego, err := resolveEgo(newEgoCommand())
	require.NoError(t, err)

	assert.Empty(t, ego, "empty flag defers to the agent package's embedded ego")
}

func TestResolveEgo_ReadsPromptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "EGO.md")
	require.NoError(t, os.WriteFile(path, []byte("You are a test ego.\n"), 0o644))
	cmd := newEgoCommand()
	require.NoError(t, cmd.Flags().Set("ego-file", path))

	ego, err := resolveEgo(cmd)
	require.NoError(t, err)

	assert.Equal(t, "You are a test ego.\n", ego)
}

func TestResolveEgo_MissingFileFailsAtStartup(t *testing.T) {
	cmd := newEgoCommand()
	require.NoError(t, cmd.Flags().Set("ego-file", filepath.Join(t.TempDir(), "nope.md")))

	_, err := resolveEgo(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ego file")
}

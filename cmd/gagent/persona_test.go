package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPersonaCommand returns a bare command carrying the --agent-prompt flag.
func newPersonaCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("agent-prompt", "", "")
	return cmd
}

func TestResolvePersona_NoFlagSelectsEmbeddedDefault(t *testing.T) {
	persona, err := resolvePersona(newPersonaCommand())
	require.NoError(t, err)

	assert.Empty(t, persona, "empty flag defers to the agent package's embedded persona")
}

func TestResolvePersona_ReadsPromptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PERSONA.md")
	require.NoError(t, os.WriteFile(path, []byte("You are a test persona.\n"), 0o644))
	cmd := newPersonaCommand()
	require.NoError(t, cmd.Flags().Set("agent-prompt", path))

	persona, err := resolvePersona(cmd)
	require.NoError(t, err)

	assert.Equal(t, "You are a test persona.\n", persona)
}

func TestResolvePersona_MissingFileFailsAtStartup(t *testing.T) {
	cmd := newPersonaCommand()
	require.NoError(t, cmd.Flags().Set("agent-prompt", filepath.Join(t.TempDir(), "nope.md")))

	_, err := resolvePersona(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent prompt file")
}

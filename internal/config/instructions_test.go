package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/config"
)

func TestResolveOperatorInstructions_NoFlagIsEmpty(t *testing.T) {
	instructions, err := config.ResolveOperatorInstructions(egoFlags())
	require.NoError(t, err)

	assert.Empty(t, instructions, "empty flag leaves the instructions out")
}

func TestResolveOperatorInstructions_ReadsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "INSTRUCTIONS.md")
	require.NoError(t, os.WriteFile(path, []byte("Answer every envelope.\n"), 0o644))
	fs := egoFlags()
	require.NoError(t, fs.Set("instructions-file", path))

	instructions, err := config.ResolveOperatorInstructions(fs)
	require.NoError(t, err)

	assert.Equal(t, "Answer every envelope.\n", instructions)
}

func TestResolveOperatorInstructions_MissingFileFailsAtStartup(t *testing.T) {
	fs := egoFlags()
	require.NoError(t, fs.Set("instructions-file", filepath.Join(t.TempDir(), "nope.md")))

	_, err := config.ResolveOperatorInstructions(fs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instructions file")
}

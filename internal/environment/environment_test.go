package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/environment"
)

func TestOpen_CreatesLayout(t *testing.T) {
	root := t.TempDir()

	env, err := environment.Open(root, "env-1")

	require.NoError(t, err)
	assert.Equal(t, "env-1", env.ID())
	assert.Equal(t, filepath.Join(root, "env-1"), env.Dir())
	assert.Equal(t, filepath.Join(root, "env-1", "workspace"), env.Workspace())
	info, err := os.Stat(env.Workspace())
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestOpen_CreateOnFirstUse(t *testing.T) {
	root := t.TempDir()

	env, err := environment.Open(root, "env-1")
	require.NoError(t, err)
	marker := filepath.Join(env.Workspace(), "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("data"), 0o644))

	reopened, err := environment.Open(root, "env-1")

	require.NoError(t, err)
	content, err := os.ReadFile(filepath.Join(reopened.Workspace(), "keep.txt"))
	require.NoError(t, err, "reopening must preserve existing contents")
	assert.Equal(t, "data", string(content))
}

func TestOpen_InvalidID(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "empty", id: ""},
		{name: "dot", id: "."},
		{name: "dot dot", id: ".."},
		{name: "forward slash", id: "a/b"},
		{name: "backslash", id: `a\b`},
		{name: "NUL byte", id: "a\x00b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()

			_, err := environment.Open(root, tt.id)

			require.Error(t, err)
			assert.ErrorIs(t, err, environment.ErrInvalidID)
			entries, readErr := os.ReadDir(root)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "invalid ID must not create directories")
		})
	}
}

func TestTeardown_RemovesDirectory(t *testing.T) {
	root := t.TempDir()
	env, err := environment.Open(root, "env-1")
	require.NoError(t, err)

	require.NoError(t, env.Teardown())

	_, err = os.Stat(env.Dir())
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestTeardown_Idempotent(t *testing.T) {
	root := t.TempDir()
	env, err := environment.Open(root, "env-1")
	require.NoError(t, err)

	require.NoError(t, env.Teardown())
	assert.NoError(t, env.Teardown())
}

func TestDefaultRoot_MatchesArchitectureDoc(t *testing.T) {
	assert.Equal(t, "data/envs", environment.DefaultRoot)
	assert.Equal(t, "workspace", environment.WorkspaceDir)
}

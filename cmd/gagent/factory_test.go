package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/sandbox"
)

func TestNewFactory_CarriesResolvedConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	contents := "image: debian:13\n" +
		"exec-timeout: 45s\n" +
		"cpu-limit: 1.5\n" +
		"memory-limit: 256m\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gagent.yaml"), []byte(contents), 0o644))
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	factory := newFactory(cfg, nil, nil, "", nil)

	assert.Equal(t, "debian:13", factory.Image)
	assert.Equal(t, 45*time.Second, factory.ExecTimeout)
	assert.Equal(t, sandbox.Policy{NanoCPUs: 1_500_000_000, MemoryBytes: 256 * 1024 * 1024}, factory.Policy,
		"the resolved quota must reach the environment factory")
}

func TestNewFactory_DefaultsBoundEveryContainer(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg, err := resolveConfig(newTestCommand(t))
	require.NoError(t, err)

	factory := newFactory(cfg, nil, nil, "", nil)

	assert.Positive(t, factory.Policy.NanoCPUs, "an unconfigured container must not get unlimited CPU")
	assert.Positive(t, factory.Policy.MemoryBytes, "an unconfigured container must not get unlimited memory")
}

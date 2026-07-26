package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testImage is the stock base image used for environment containers.
const testImage = "alpine:3.21"

// startFailImage has no tail binary, so ContainerCreate succeeds but
// ContainerStart always fails with "executable file not found".
const startFailImage = "hello-world"

// requireDocker gates daemon-dependent tests on the GAGENT_DOCKER_TESTS
// environment variable so CI and machines without Docker skip cleanly. With
// the variable set, an unreachable daemon is a failure, not a skip.
func requireDocker(t *testing.T) *DockerRuntime {
	t.Helper()
	if os.Getenv("GAGENT_DOCKER_TESTS") == "" {
		t.Skip("set GAGENT_DOCKER_TESTS=1 to run Docker daemon tests")
	}
	rt, err := NewDockerRuntime()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = rt.cli.Ping(ctx)
	require.NoError(t, err, "GAGENT_DOCKER_TESTS is set but the Docker daemon is unreachable")
	return rt
}

// createTestContainer starts a container with its own workspace and
// registers cleanup. The workspace lives under $HOME rather than the
// default temp dir because Docker Desktop VMs cannot bind-mount /tmp.
func createTestContainer(t *testing.T, rt *DockerRuntime, policy Policy) (string, string) {
	t.Helper()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	workspace, err := os.MkdirTemp(home, "gagent-sandbox-test-")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(workspace)) })
	envID := "test-" + t.Name()
	// Remove leftovers from previous failed runs.
	_ = rt.cli.ContainerRemove(context.Background(), containerName(envID), container.RemoveOptions{Force: true})
	id, err := rt.Create(context.Background(), Config{
		EnvID:     envID,
		Workspace: workspace,
		Image:     testImage,
		Policy:    policy,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, rt.Destroy(context.Background(), id))
	})
	return id, workspace
}

func TestDockerRuntime_ExecWritesToWorkspace(t *testing.T) {
	rt := requireDocker(t)
	id, workspace := createTestContainer(t, rt, Policy{})

	res, err := rt.Exec(context.Background(), id,
		[]string{"/bin/sh", "-c", "echo hello > /workspace/out.txt"}, 10*time.Second)

	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	content, err := os.ReadFile(filepath.Join(workspace, "out.txt"))
	require.NoError(t, err, "file written via exec must land in the env workspace")
	assert.Equal(t, "hello\n", string(content))
}

func TestDockerRuntime_MemoryDBUnreachable(t *testing.T) {
	rt := requireDocker(t)
	id, workspace := createTestContainer(t, rt, Policy{})
	// memory.db lives in the env directory next to the workspace and must
	// never be reachable from inside the container.
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(workspace), "memory.db"), []byte("secret-rows"), 0o600))

	res, err := rt.Exec(context.Background(), id,
		[]string{"/bin/sh", "-c", "find / -name memory.db 2>/dev/null; exit 0"}, 30*time.Second)

	require.NoError(t, err)
	assert.Empty(t, res.Stdout, "memory.db must not be reachable from inside the container")
}

func TestDockerRuntime_ContainerConfigEnforcesPolicy(t *testing.T) {
	rt := requireDocker(t)
	policy := Policy{NanoCPUs: 1_000_000_000, MemoryBytes: 256 * 1024 * 1024}
	id, workspace := createTestContainer(t, rt, policy)

	info, err := rt.cli.ContainerInspect(context.Background(), id)

	require.NoError(t, err)
	assert.Equal(t, container.NetworkMode("none"), info.HostConfig.NetworkMode, "container must have no network egress")
	assert.Equal(t, policy.NanoCPUs, info.HostConfig.NanoCPUs)
	assert.Equal(t, policy.MemoryBytes, info.HostConfig.Memory)
	assert.Equal(t, []string{workspace + ":" + workspaceMountPath}, info.HostConfig.Binds,
		"the workspace must be the only mount")
}

func TestDockerRuntime_ExecTimeout(t *testing.T) {
	rt := requireDocker(t)
	id, _ := createTestContainer(t, rt, Policy{})

	res, err := rt.Exec(context.Background(), id,
		[]string{"/bin/sh", "-c", "sleep 30"}, time.Second)

	require.NoError(t, err)
	assert.True(t, res.TimedOut)
	assert.Equal(t, -1, res.ExitCode)
}

func TestDockerRuntime_CreateStartFailureLeavesNoContainer(t *testing.T) {
	rt := requireDocker(t)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	workspace, err := os.MkdirTemp(home, "gagent-sandbox-test-")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, os.RemoveAll(workspace)) })
	envID := "test-" + t.Name()
	// Remove leftovers from previous failed runs.
	_ = rt.cli.ContainerRemove(context.Background(), containerName(envID), container.RemoveOptions{Force: true})

	_, err = rt.Create(context.Background(), Config{
		EnvID:     envID,
		Workspace: workspace,
		Image:     startFailImage,
	})

	require.Error(t, err)
	_, inspectErr := rt.cli.ContainerInspect(context.Background(), containerName(envID))
	assert.True(t, errdefs.IsNotFound(inspectErr),
		"a failed Create must not leak the container, inspect returned: %v", inspectErr)
}

func TestDockerRuntime_DestroyUnknownContainer(t *testing.T) {
	rt := requireDocker(t)

	assert.NoError(t, rt.Destroy(context.Background(), "no-such-container"))
}

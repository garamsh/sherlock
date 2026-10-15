package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// imageLayoutTools is the canonical first-party tool set (ADR 0021).
// Tests against this list fail when a tool is renamed or removed and
// fail the build rather than ship without a manifest.
var imageLayoutTools = []string{
	"file_list",
	"file_read",
	"file_write",
	"github_comment",
	"github_open_issue",
	"github_open_pull_request",
	"github_push",
	"message_send",
	"shell_exec",
}

// TestImageLayout_AgentCarriesBakedTools verifies ADR 0021: the sherlock-agent
// image carries the first-party tool executables at /opt/sherlock/tools/, with
// the modes the image Dockerfile sets and the manifest each tool ships beside
// its executable. The test builds the image with `docker build`, creates a
// stopped container from it with `docker create`, and copies the tree out with
// `docker cp`. No container is started; the inspection is offline.
//
// Opt-in via SHERLOCK_TEST_IMAGE_LAYOUT=1; the build is gated because
// `docker build` is e2e (real Docker, a non-trivial dependency), and because
// the image itself is what the test exercises, not its in-process modules.
func TestImageLayout_AgentCarriesBakedTools(t *testing.T) {
	if os.Getenv("SHERLOCK_TEST_IMAGE_LAYOUT") != "1" {
		t.Skip("image layout test requires SHERLOCK_TEST_IMAGE_LAYOUT=1; it runs `docker build` against the agent Dockerfile")
	}
	dockerPath, err := exec.LookPath("docker")
	require.NoError(t, err, "docker must be on PATH for SHERLOCK_TEST_IMAGE_LAYOUT=1")

	moduleRoot := moduleRoot(t)

	imageTag := "sherlock-agent-layout-test:tmp"
	buildCmd := exec.Command(dockerPath, "build",
		"-f", filepath.Join(moduleRoot, "build/agent.Dockerfile"),
		"-t", imageTag,
		moduleRoot,
	)
	buildCmd.Stdout = os.Stderr
	buildCmd.Stderr = os.Stderr
	require.NoError(t, buildCmd.Run(), "docker build -f build/agent.Dockerfile failed; output above")

	containerName := "sherlock-agent-layout-test-container"
	t.Cleanup(func() {
		rmCmd := exec.Command(dockerPath, "rm", "-f", containerName)
		rmCmd.Stdout = os.Stderr
		rmCmd.Stderr = os.Stderr
		_ = rmCmd.Run()
		rmiCmd := exec.Command(dockerPath, "rmi", imageTag)
		rmiCmd.Stdout = os.Stderr
		rmiCmd.Stderr = os.Stderr
		_ = rmiCmd.Run()
	})

	createCmd := exec.Command(dockerPath, "create",
		"--name", containerName,
		"--entrypoint", "/bin/true",
		imageTag,
	)
	createCmd.Stdout = os.Stderr
	createCmd.Stderr = os.Stderr
	require.NoError(t, createCmd.Run(), "docker create failed; output above")

	tmpDir := t.TempDir()
	dstTools := filepath.Join(tmpDir, "tools")
	require.NoError(t, os.MkdirAll(dstTools, 0o755))
	cpCmd := exec.Command(dockerPath, "cp",
		containerName+":/opt/sherlock/tools/.",
		dstTools,
	)
	cpCmd.Stdout = os.Stderr
	cpCmd.Stderr = os.Stderr
	require.NoError(t, cpCmd.Run(), "docker cp failed; output above")

	for _, name := range imageLayoutTools {
		t.Run(name, func(t *testing.T) {
			execPath := filepath.Join(dstTools, name, name)
			execInfo, err := os.Stat(execPath)
			require.NoError(t, err, "agent image carries no executable at /opt/sherlock/tools/%s/%s", name, name)
			assert.Equal(t, os.FileMode(0o755), execInfo.Mode().Perm(),
				"tool executable /opt/sherlock/tools/%s/%s must be mode 0755; got %v",
				name, name, execInfo.Mode().Perm())

			manifestPath := filepath.Join(dstTools, name, "manifest.json")
			manifestInfo, err := os.Stat(manifestPath)
			require.NoError(t, err, "agent image carries no manifest.json at /opt/sherlock/tools/%s/", name)
			assert.Equal(t, os.FileMode(0o644), manifestInfo.Mode().Perm(),
				"tool manifest /opt/sherlock/tools/%s/manifest.json must be mode 0644; got %v",
				name, manifestInfo.Mode().Perm())
		})
	}
}

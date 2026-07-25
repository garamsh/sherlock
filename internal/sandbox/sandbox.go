package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Policy is the boundary policy injected at environment creation
// (docs/architecture/environment.md): resource quotas the agent cannot
// change. The zero value means no explicit limits.
type Policy struct {
	// NanoCPUs limits CPU usage (1e9 = one CPU). Zero means unlimited.
	NanoCPUs int64
	// MemoryBytes limits container memory. Zero means unlimited.
	MemoryBytes int64
}

// Config describes one environment container: which workspace to mount
// and under which policy.
type Config struct {
	// EnvID names the container; it must be unique per environment.
	EnvID string
	// Workspace is the host path bind-mounted as the container's only
	// mount. It must exist.
	Workspace string
	// Image is the container image reference.
	Image string
	// Policy bounds CPU and memory.
	Policy Policy
}

// Result is the outcome of one exec. TimedOut reports that the command
// exceeded its timeout; in that case ExitCode is -1 and Stdout/Stderr
// hold whatever was captured before the deadline.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

// Runtime owns the lifecycle of environment containers: create, exec,
// destroy. Implementations must guarantee the boundary invariants:
// workspace-only mount, no network egress, injected resource limits.
type Runtime interface {
	// Create starts a container for cfg and returns its ID.
	Create(ctx context.Context, cfg Config) (string, error)
	// Exec runs command inside the container, returning when the command
	// finishes or the timeout expires. A timeout yields a Result with
	// TimedOut set and a nil error.
	Exec(ctx context.Context, id string, command []string, timeout time.Duration) (Result, error)
	// Destroy removes the container. Destroying an unknown container is
	// not an error.
	Destroy(ctx context.Context, id string) error
}

// workspaceMountPath is where the environment workspace is mounted inside
// the container. It is the container's only mount.
const workspaceMountPath = "/workspace"

// validate rejects configs that cannot form a valid container.
func (c Config) validate() error {
	if c.EnvID == "" {
		return errors.New("sandbox: config EnvID is required")
	}
	if c.Workspace == "" {
		return errors.New("sandbox: config Workspace is required")
	}
	if c.Image == "" {
		return errors.New("sandbox: config Image is required")
	}
	return nil
}

// containerName derives the Docker container name for an environment.
func containerName(envID string) string {
	return fmt.Sprintf("gagent-env-%s", envID)
}

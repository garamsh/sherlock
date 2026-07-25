package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	imagetypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerRuntime implements Runtime against a local Docker daemon via the
// official Docker Go SDK. The zero value is not usable; use NewDockerRuntime.
type DockerRuntime struct {
	cli *client.Client
}

// NewDockerRuntime returns a Runtime talking to the daemon configured by the
// standard Docker environment (DOCKER_HOST and friends).
func NewDockerRuntime() (*DockerRuntime, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &DockerRuntime{cli: cli}, nil
}

// Create starts a container with the workspace as its only bind mount, no
// network access, and the injected CPU/memory policy, then returns its ID.
func (r *DockerRuntime) Create(ctx context.Context, cfg Config) (string, error) {
	if err := cfg.validate(); err != nil {
		return "", err
	}
	if err := r.ensureImage(ctx, cfg.Image); err != nil {
		return "", err
	}
	resp, err := r.cli.ContainerCreate(ctx,
		&container.Config{
			Image: cfg.Image,
			// Keep the container alive so Exec can attach repeatedly.
			Cmd:        []string{"tail", "-f", "/dev/null"},
			WorkingDir: workspaceMountPath,
		},
		&container.HostConfig{
			Binds:       []string{cfg.Workspace + ":" + workspaceMountPath},
			NetworkMode: "none",
			Resources: container.Resources{
				NanoCPUs: cfg.Policy.NanoCPUs,
				Memory:   cfg.Policy.MemoryBytes,
			},
		},
		nil, nil, containerName(cfg.EnvID))
	if err != nil {
		return "", fmt.Errorf("create container for env %q: %w", cfg.EnvID, err)
	}
	if err := r.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container for env %q: %w", cfg.EnvID, err)
	}
	return resp.ID, nil
}

// Exec runs command inside the container and captures stdout/stderr. If the
// command does not finish within timeout, the returned Result has TimedOut
// set and ExitCode -1; the error is nil.
func (r *DockerRuntime) Exec(ctx context.Context, id string, command []string, timeout time.Duration) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	exec, err := r.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          command,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return Result{}, fmt.Errorf("create exec in %q: %w", id, err)
	}
	resp, err := r.cli.ContainerExecAttach(ctx, exec.ID, container.ExecAttachOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("attach exec in %q: %w", id, err)
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Docker multiplexes stdout/stderr on the hijacked stream.
		_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		// Closing the connection unblocks the copy above; the exec process
		// may linger in the container until Destroy removes it.
		resp.Close()
		<-done
		return Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: -1, TimedOut: true}, nil
	}

	inspect, err := r.cli.ContainerExecInspect(ctx, exec.ID)
	if err != nil {
		return Result{}, fmt.Errorf("inspect exec in %q: %w", id, err)
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: inspect.ExitCode}, nil
}

// Destroy force-removes the container. An unknown container is not an error.
func (r *DockerRuntime) Destroy(ctx context.Context, id string) error {
	if err := r.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("destroy container %q: %w", id, err)
	}
	return nil
}

// ensureImage pulls the image when the daemon does not have it locally.
func (r *DockerRuntime) ensureImage(ctx context.Context, image string) error {
	_, err := r.cli.ImageInspect(ctx, image)
	if err == nil {
		return nil
	}
	if !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect image %q: %w", image, err)
	}
	reader, err := r.cli.ImagePull(ctx, image, imagetypes.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return fmt.Errorf("pull image %q: %w", image, err)
	}
	return nil
}

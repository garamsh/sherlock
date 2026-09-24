package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"regexp"
	"time"

	"google.golang.org/grpc"

	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/files"
	"github.com/garamsh/gagent/internal/workspace/shell"
)

// ServeOptions is what serving the data plane needs, declared here
// rather than taken as the entry point's settings type: the workspace
// is a boundary, and a boundary states its own surface instead of
// depending on the layer that composes the system.
type ServeOptions struct {
	// ExecNetwork grants network access independently of the caller.
	ExecNetwork bool
	// ExecEnv names variables read from the workspace environment at startup.
	ExecEnv []string
	// Addr is the gRPC listener's address. It must already be one the
	// caller has decided may be served — the listener carries no
	// authentication in this phase, so a non-loopback bind is a remote
	// shell into the workspace (ADR 0012).
	Addr string
	// Dir is the workspace directory, created on first use.
	Dir string
	// ExecUID is the UID exec children run as (ADR 0008).
	ExecUID int
	// ExecTimeout is the per-call ceiling for Exec commands.
	ExecTimeout time.Duration
}

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Serve runs the workspace's gRPC listener until ctx is done.
//
// The workspace is the data plane (ADR 0017): it owns the workspace
// directory, serves Read/Write/List, and runs Exec children through its
// own executor — the agent executes nothing itself. It chowns its root
// to the exec UID when privileged, so files written through the gRPC
// contract and by exec'd children share one owner.
func Serve(ctx context.Context, opts ServeOptions, logger *slog.Logger) error {
	execEnv := make([]string, 0, len(opts.ExecEnv))
	for _, name := range opts.ExecEnv {
		if !environmentName.MatchString(name) {
			return fmt.Errorf("exec-env must contain environment variable names, not values")
		}
		value, ok := os.LookupEnv(name)
		if !ok {
			return fmt.Errorf("exec-env variable %s is not set in the workspace", name)
		}
		execEnv = append(execEnv, name+"="+value)
	}
	// Single-account custody (ADR 0003): when exec-uid is the
	// workspace's own account, exec children share it, so the
	// workspace's /proc environ would be readable to them — close that
	// channel.
	if opts.ExecUID == os.Geteuid() {
		if err := shell.ProtectEnviron(); err != nil {
			return err
		}
	}

	workspaceFS, err := files.Open(opts.Dir)
	if err != nil {
		return err
	}
	if err := workspaceFS.HandOver(opts.ExecUID); err != nil {
		return err
	}

	listener, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	workspace.RegisterWorkspaceServer(grpcServer, &Server{
		FS:          workspaceFS,
		ExecNetwork: opts.ExecNetwork,
		ExecEnv:     execEnv,
		Executor:    shell.NewProcessExecutor(opts.ExecUID, opts.ExecTimeout),
	})

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	logger.Info("workspace listening",
		"op", "startup", "addr", opts.Addr, "workspace", workspaceFS.Root(),
		"warning", "no authentication: pod-local listener")
	if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		logger.Error("workspace server failed", "op", "serve", "error", err)
		return err
	}
	return nil
}

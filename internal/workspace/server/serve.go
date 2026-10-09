package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/garamsh/sherlock/internal/workspace"
	"github.com/garamsh/sherlock/internal/workspace/files"
	"github.com/garamsh/sherlock/internal/workspace/shell"
)

// ServeOptions is what serving the data plane needs, declared here
// rather than taken as the entry point's settings type: the workspace
// is a boundary, and a boundary states its own surface instead of
// depending on the layer that composes the system.
type ServeOptions struct {
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

// Serve runs the workspace's gRPC listener until ctx is done.
//
// The workspace is the data plane (ADR 0017): it owns the workspace
// directory, serves Read/Write/List, and runs Exec children through its
// own executor — the agent executes nothing itself. It chowns its root
// to the exec UID when privileged, so files written through the gRPC
// contract and by exec'd children share one owner.
func Serve(ctx context.Context, opts ServeOptions, logger *slog.Logger) error {
	if euid := os.Geteuid(); euid != 0 && opts.ExecUID == euid {
		logger.Info("unprivileged shape: the workspace runs unprivileged and exec-uid is its own uid, so Exec children run as the workspace's account; it requires the agent's volume kept out of the workspace container, no process namespace shared with the agent container, and a host that admits the unprivileged user and network namespace clone (docs/architecture/deployment.md §Volumes and ownership)",
			"op", "startup", "exec_uid", opts.ExecUID)
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
		FS:       workspaceFS,
		Executor: shell.NewProcessExecutor(opts.ExecUID, opts.ExecTimeout),
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

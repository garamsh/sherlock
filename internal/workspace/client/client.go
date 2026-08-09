// Package client is the typed workspace gRPC client tools import. The
// gRPC server (workspace.Server) is internal/workspace's other
// surface; this package exposes the matching client connection helper
// so agent-side tools can construct a workspace client from a single
// address without reaching into grpc.NewClient directly.
//
// The transport is plain gRPC over loopback in this phase — the
// listener is unauthenticated and serves pod-local traffic only (ADR
// 0018's open authentication question).
package client

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/gagent/internal/workspace"
)

// New dials addr over plaintext gRPC and returns the workspace client
// together with a Close function that shuts the connection down. The
// returned client is the same WorkspaceClient the gRPC server satisfies.
func New(ctx context.Context, addr string) (workspace.WorkspaceClient, func() error, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("workspace client: dial %q: %w", addr, err)
	}
	return workspace.NewWorkspaceClient(conn), conn.Close, nil
}

// NewWithConn returns a workspace client wrapping conn, plus a Close
// function that does not close conn (the caller owns the connection).
// Useful in tests where one dial serves multiple clients.
func NewWithConn(conn grpc.ClientConnInterface) (workspace.WorkspaceClient, func() error, error) {
	if conn == nil {
		return nil, nil, fmt.Errorf("workspace client: nil connection")
	}
	return workspace.NewWorkspaceClient(conn), func() error { return nil }, nil
}

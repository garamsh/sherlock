package client_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/internal/workspace/client"
)

// TestNew_DialAndClose verifies the client wrapper opens a connection
// to a free loopback listener and closes it cleanly. The listener
// registers the workspace gRPC service so the dial has something to
// talk to.
func TestNew_DialAndClose(t *testing.T) {
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	srv := grpc.NewServer()
	workspace.RegisterWorkspaceServer(srv, &nopServer{})
	go func() { _ = srv.Serve(listenOn(addr)) }()
	defer srv.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ws, closeFn, err := client.New(ctx, addr)
	require.NoError(t, err)
	require.NotNil(t, ws)
	require.NoError(t, closeFn())
}

// nopServer satisfies workspace.WorkspaceServer with no-op handlers so
// the gRPC dispatch machinery exercises its wiring on every dial.
type nopServer struct {
	workspace.UnimplementedWorkspaceServer
}

// listenOn dials an in-process TCP listener bound to addr, returning
// it for serving. The listener is created once outside the goroutine
// so the test can race-free close it later.
func listenOn(addr string) net.Listener {
	listen, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}
	return listen
}

func (nopServer) Read(context.Context, *workspace.ReadRequest) (*workspace.ReadResponse, error) {
	return &workspace.ReadResponse{}, nil
}

func (nopServer) Write(context.Context, *workspace.WriteRequest) (*workspace.WriteResponse, error) {
	return &workspace.WriteResponse{}, nil
}

func (nopServer) List(context.Context, *workspace.ListRequest) (*workspace.ListResponse, error) {
	return &workspace.ListResponse{}, nil
}

// TestNewWithConn_NilRejects pins the boundary: a nil conn is a
// caller mistake; the helper returns an error rather than panicking.
func TestNewWithConn_NilRejects(t *testing.T) {
	_, _, err := client.NewWithConn(nil)
	assert.Error(t, err)
}

package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/garamsh/gagent/internal/workspace/files"
	"github.com/garamsh/gagent/internal/workspace/shell"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is the gRPC workspace backend: Read, Write, and List against
// the workspace filesystem, and Exec through the workspace's executor
// (ADR 0017) — the workspace is the computer: it serves the files and
// runs the agent-influenced processes. It exposes no tool hosting:
// tools are executables the agent's tool client spawns from their
// manifests. The plugin concept is dropped.
type Server struct {
	UnimplementedWorkspaceServer
	FS       *files.FS
	Executor shell.Executor
}

func (s *Server) Read(_ context.Context, req *ReadRequest) (*ReadResponse, error) {
	content, err := s.FS.Read(req.GetPath())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &ReadResponse{Content: content}, nil
}

func (s *Server) Write(_ context.Context, req *WriteRequest) (*WriteResponse, error) {
	if err := s.FS.Write(req.GetPath(), req.GetContent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &WriteResponse{}, nil
}

func (s *Server) List(_ context.Context, req *ListRequest) (*ListResponse, error) {
	entries, err := s.FS.List(req.GetPath())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	out := &ListResponse{Entries: make([]*Entry, 0, len(entries))}
	for _, entry := range entries {
		out.Entries = append(out.Entries, &Entry{Name: entry.Name, Dir: entry.Dir})
	}
	return out, nil
}

// Exec runs the command through the workspace's executor. An empty
// workdir defaults to the workspace directory, so exec'd programs see
// the files the filesystem RPCs serve. A timeout is a normal outcome —
// the response carries timed_out: true (ADR 0008) — while a privilege
// drop the platform cannot perform fails closed with
// agent_uid unavailable.
func (s *Server) Exec(ctx context.Context, req *ExecRequest) (*ExecResponse, error) {
	workdir := req.GetWorkdir()
	if workdir == "" {
		workdir = s.FS.Root()
	}
	result, err := s.Executor.Run(ctx, shell.Request{
		Command:   req.GetCommand(),
		Args:      req.GetArgs(),
		Stdin:     req.GetStdin(),
		Timeout:   time.Duration(req.GetTimeoutNanos()),
		Workdir:   workdir,
		Env:       req.GetEnv(),
		NoNetwork: req.GetNoNetwork(),
		Trusted:   req.GetTrusted(),
	})
	if errors.Is(err, shell.ErrTimedOut) {
		result.TimedOut = true
		return execResponse(result), nil
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return execResponse(result), nil
}

func execResponse(result shell.Result) *ExecResponse {
	return &ExecResponse{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: int32(result.ExitCode),
		TimedOut: result.TimedOut,
	}
}

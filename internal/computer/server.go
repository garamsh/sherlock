package computer

import (
	"context"
	"time"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/shell"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	UnimplementedComputerServer
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
func (s *Server) Exec(ctx context.Context, req *ExecRequest) (*ExecResponse, error) {
	result, err := s.Executor.Run(ctx, shell.Request{Command: req.GetCommand(), Args: req.GetArgs(), Stdin: req.GetStdin(), Timeout: duration(req.GetTimeoutNanos()), Workdir: req.GetWorkdir(), Env: req.GetEnv(), NoNetwork: req.GetNoNetwork(), Trusted: req.GetTrusted()})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &ExecResponse{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: int32(result.ExitCode), TimedOut: result.TimedOut}, nil
}

func duration(n int64) (d time.Duration) { return time.Duration(n) }

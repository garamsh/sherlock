package workspace

import (
	"context"

	"github.com/garamsh/gagent/internal/files"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server is the gRPC workspace backend: Read, Write, and List against the
// workspace filesystem (ADR 0018). It exposes no process execution and no
// tool hosting — process exec is the agent's native capability, and tools
// are agent-side Go packages the agent imports at compile time. The
// plugin concept is dropped.
type Server struct {
	UnimplementedWorkspaceServer
	FS *files.FS
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

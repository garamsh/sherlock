package files_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/tooltest"
	"github.com/garamsh/gagent/internal/workspace"
	"github.com/garamsh/gagent/tools/files"
)

// schemaFile mirrors the embed in tools/files/files.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "files", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(files.InputSchema()) == 0 {
		t.Fatal("files.InputSchema returned empty bytes")
	}
	if len(files.OutputSchema()) == 0 {
		t.Fatal("files.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match CallInput's
// json tags. The output schema is an unconstrained object, so the
// Read/Write/ListOutput structs have no schema shape to check against.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, _ := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(files.CallInput{}))
}

// fakeWorkspaceClient is the in-memory workspace gRPC client the
// tool's logic is tested against. Each method dispatches against a
// small in-memory map; the unit under test is the tool's wire-to-API
// translation, not the workspace's filesystem implementation (that
// lives in internal/files and is covered there).
type fakeWorkspaceClient struct {
	workspace.WorkspaceClient
	files   map[string][]byte
	dirs    map[string][]files.Entry
	readErr error
}

func newFakeWorkspaceClient() *fakeWorkspaceClient {
	return &fakeWorkspaceClient{
		files: map[string][]byte{},
		dirs:  map[string][]files.Entry{},
	}
}

func (f *fakeWorkspaceClient) Read(_ context.Context, req *workspace.ReadRequest, _ ...grpc.CallOption) (*workspace.ReadResponse, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	content, ok := f.files[req.GetPath()]
	if !ok {
		return nil, errors.New("not found")
	}
	return &workspace.ReadResponse{Content: content}, nil
}

func (f *fakeWorkspaceClient) Write(_ context.Context, req *workspace.WriteRequest, _ ...grpc.CallOption) (*workspace.WriteResponse, error) {
	f.files[req.GetPath()] = req.GetContent()
	return &workspace.WriteResponse{}, nil
}

func (f *fakeWorkspaceClient) List(_ context.Context, req *workspace.ListRequest, _ ...grpc.CallOption) (*workspace.ListResponse, error) {
	entries := f.dirs[req.GetPath()]
	out := &workspace.ListResponse{Entries: make([]*workspace.Entry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, &workspace.Entry{Name: e.Name, Dir: e.Dir})
	}
	return out, nil
}

func TestFilesTool_InvalidInputRejectedAtBoundary(t *testing.T) {
	ws := newFakeWorkspaceClient()
	filesTool := files.New(ws)

	tests := []struct {
		name  string
		input string
	}{
		{name: "missing operation", input: `{"path":"a.txt"}`},
		{name: "missing path", input: `{"operation":"read"}`},
		{name: "unknown operation", input: `{"operation":"delete","path":"a.txt"}`},
		{name: "write without content", input: `{"operation":"write","path":"a.txt"}`},
		{name: "additional field", input: `{"operation":"read","path":"a.txt","mode":"fast"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Invoke(context.Background(), filesTool, json.RawMessage(tt.input))
			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
		})
	}
}

func TestFilesTool_WriteThenRead(t *testing.T) {
	ws := newFakeWorkspaceClient()
	filesTool := files.New(ws)

	out, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"write","path":"notes/a.txt","content":"hello"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"bytes_written":5}`, string(out))

	out, err = tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"notes/a.txt"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"hello"}`, string(out))
}

func TestFilesTool_List(t *testing.T) {
	ws := newFakeWorkspaceClient()
	ws.dirs["."] = []files.Entry{{Name: "dir", Dir: true}}
	filesTool := files.New(ws)

	_, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"list","path":"."}`))
	require.NoError(t, err)
}

func TestFilesTool_TraversalIsExecutionError(t *testing.T) {
	ws := newFakeWorkspaceClient()
	ws.readErr = errors.New("path escapes workspace")
	filesTool := files.New(ws)

	_, err := tool.Invoke(context.Background(), filesTool,
		json.RawMessage(`{"operation":"read","path":"../secret.txt"}`))
	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
}

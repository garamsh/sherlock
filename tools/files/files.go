// Package files is the agent's files tool (ADR 0018): a Go package the
// agent imports and registers as a tool.Tool at compile time. Read,
// Write, and List travel through the workspace gRPC client — the
// workspace is the data plane, the agent hosts the tool.
package files

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/workspace"
)

// ToolName is the registered name of the files tool.
const ToolName = "files"

// schemaFile holds the tool's input and output JSON Schemas side by
// side. The conformance test (internal/tool/tooltest) reads the same
// shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

// schemaDoc mirrors schema.json's on-disk shape; the raw messages keep
// the embedded bytes untouched straight through to the LLM-facing
// tool.Tool contract.
type schemaDoc struct {
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

// schema is the parsed view of schemaFile. A package-level parse keeps
// the embedded bytes a single source of truth and a single failure
// point if the file is malformed.
var schema = func() schemaDoc {
	var doc schemaDoc
	if err := json.Unmarshal(schemaFile, &doc); err != nil {
		panic("files: embedded schema.json is malformed: " + err.Error())
	}
	return doc
}()

// InputSchema returns the JSON Schema for the files tool's input.
func InputSchema() json.RawMessage { return schema.Input }

// OutputSchema returns the JSON Schema for the files tool's output.
func OutputSchema() json.RawMessage { return schema.Output }

// Tool is the agent's files tool: it reads, writes, and lists files
// inside the workspace through the gRPC client the agent wired in.
type Tool struct {
	ws workspace.WorkspaceClient
}

// New returns a files tool that talks to the workspace through ws.
// The agent constructs one and registers it in the runtime tool
// registry at startup; there is no runtime plugin loader (ADR 0018).
func New(ws workspace.WorkspaceClient) *Tool {
	return &Tool{ws: ws}
}

// Name returns the tool name.
func (t *Tool) Name() string { return ToolName }

// Description returns the one-sentence tool summary.
func (t *Tool) Description() string {
	return "Read, write, and list files inside the agent's workspace."
}

// InputSchema returns the JSON Schema for the tool input.
func (t *Tool) InputSchema() json.RawMessage { return InputSchema() }

// OutputSchema returns the JSON Schema for the tool result.
func (t *Tool) OutputSchema() json.RawMessage { return OutputSchema() }

// CallInput is the canonical typed form of the files tool's input
// schema; the schema/struct consistency test keeps them in lockstep.
type CallInput struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// ReadOutput is the result of the read operation.
type ReadOutput struct {
	Content string `json:"content"`
}

// WriteOutput is the result of the write operation.
type WriteOutput struct {
	BytesWritten int `json:"bytes_written"`
}

// Entry mirrors the workspace gRPC Entry message but strips the
// protobuf typing: a JSON shape the model can read.
type Entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

// ListOutput is the result of the list operation.
type ListOutput struct {
	Entries []Entry `json:"entries"`
}

// Call dispatches the requested operation through the workspace gRPC
// client. Input has already passed schema validation at the contract
// boundary.
func (t *Tool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

// doCall routes the decoded input to the operation it names. The
// result type varies per operation, so the output is any; the output
// schema is an unconstrained object.
func (t *Tool) doCall(ctx context.Context, args CallInput) (any, error) {
	switch args.Operation {
	case "read":
		return t.read(ctx, args.Path)
	case "write":
		return t.write(ctx, args.Path, args.Content)
	case "list":
		return t.list(ctx, args.Path)
	default:
		return nil, fmt.Errorf("unknown operation %q", args.Operation)
	}
}

func (t *Tool) read(ctx context.Context, path string) (ReadOutput, error) {
	resp, err := t.ws.Read(ctx, pathToReq(path))
	if err != nil {
		return ReadOutput{}, err
	}
	return ReadOutput{Content: string(resp.Content)}, nil
}

func (t *Tool) write(ctx context.Context, path, content string) (WriteOutput, error) {
	if _, err := t.ws.Write(ctx, &workspace.WriteRequest{Path: path, Content: []byte(content)}); err != nil {
		return WriteOutput{}, err
	}
	return WriteOutput{BytesWritten: len(content)}, nil
}

func (t *Tool) list(ctx context.Context, path string) (ListOutput, error) {
	resp, err := t.ws.List(ctx, &workspace.ListRequest{Path: path})
	if err != nil {
		return ListOutput{}, err
	}
	entries := make([]Entry, 0, len(resp.Entries))
	for _, e := range resp.Entries {
		entries = append(entries, Entry{Name: e.GetName(), Dir: e.GetDir()})
	}
	return ListOutput{Entries: entries}, nil
}

// pathToReq builds the gRPC ReadRequest for the given path.
func pathToReq(path string) *workspace.ReadRequest {
	return &workspace.ReadRequest{Path: path}
}

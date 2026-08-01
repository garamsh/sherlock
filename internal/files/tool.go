package files

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/garamsh/gagent/internal/tool"
)

// ToolName is the registered name of the files tool.
const ToolName = "files"

// toolDescription is the one-sentence summary advertised to model
// providers, matching the files responsibility in docs/architecture.
const toolDescription = "Read, write, and list files inside the environment's workspace."

// Tool is the files tool: it reads, writes, and lists files inside the
// environment workspace.
type Tool struct {
	fs *FS
}

// NewTool returns a files tool operating on fs.
func NewTool(fs *FS) *Tool {
	return &Tool{fs: fs}
}

// Name returns the tool name.
func (t *Tool) Name() string { return ToolName }

// Description returns the one-sentence tool summary.
func (t *Tool) Description() string { return toolDescription }

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

// ListOutput is the result of the list operation.
type ListOutput struct {
	Entries []Entry `json:"entries"`
}

// Call dispatches the requested operation against the workspace. Input has
// already passed schema validation at the contract boundary.
func (t *Tool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

// doCall routes the decoded input to the operation it names. The result
// type varies per operation, so the output is any; the output schema is
// an unconstrained object.
func (t *Tool) doCall(_ context.Context, args CallInput) (any, error) {
	switch args.Operation {
	case "read":
		return t.read(args.Path)
	case "write":
		return t.write(args.Path, args.Content)
	case "list":
		return t.list(args.Path)
	default:
		// Unreachable: the input schema restricts operation to the enum.
		return nil, fmt.Errorf("unknown operation %q", args.Operation)
	}
}

func (t *Tool) read(path string) (ReadOutput, error) {
	content, err := t.fs.Read(path)
	if err != nil {
		return ReadOutput{}, err
	}
	return ReadOutput{Content: string(content)}, nil
}

func (t *Tool) write(path, content string) (WriteOutput, error) {
	if err := t.fs.Write(path, []byte(content)); err != nil {
		return WriteOutput{}, err
	}
	return WriteOutput{BytesWritten: len(content)}, nil
}

func (t *Tool) list(path string) (ListOutput, error) {
	entries, err := t.fs.List(path)
	if err != nil {
		return ListOutput{}, err
	}
	return ListOutput{Entries: entries}, nil
}

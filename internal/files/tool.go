package files

import (
	"context"
	"encoding/json"
	"fmt"
)

const filesInputSchema = `{
	"type": "object",
	"properties": {
		"operation": {"type": "string", "enum": ["read", "write", "list"]},
		"path": {"type": "string", "minLength": 1},
		"content": {"type": "string"}
	},
	"required": ["operation", "path"],
	"if": {"properties": {"operation": {"const": "write"}}, "required": ["operation"]},
	"then": {"required": ["content"]},
	"additionalProperties": false
}`

const filesOutputSchema = `{
	"type": "object"
}`

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
func (t *Tool) InputSchema() json.RawMessage { return json.RawMessage(filesInputSchema) }

// OutputSchema returns the JSON Schema for the tool result.
func (t *Tool) OutputSchema() json.RawMessage { return json.RawMessage(filesOutputSchema) }

// Call dispatches the requested operation against the workspace. Input has
// already passed schema validation at the contract boundary.
func (t *Tool) Call(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	var args struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
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

func (t *Tool) read(path string) (json.RawMessage, error) {
	content, err := t.fs.Read(path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Content string `json:"content"`
	}{Content: string(content)})
}

func (t *Tool) write(path, content string) (json.RawMessage, error) {
	if err := t.fs.Write(path, []byte(content)); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		BytesWritten int `json:"bytes_written"`
	}{BytesWritten: len(content)})
}

func (t *Tool) list(path string) (json.RawMessage, error) {
	entries, err := t.fs.List(path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Entries []Entry `json:"entries"`
	}{Entries: entries})
}

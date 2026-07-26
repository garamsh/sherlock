package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpTool adapts one discovered MCP tool to tool.Tool: Call forwards
// tools/call to the owning server and maps the result back.
type mcpTool struct {
	server       *Server
	name         string
	inputSchema  json.RawMessage
	outputSchema json.RawMessage
	timeout      time.Duration
}

// newTool returns the adapter for one tool of the server.
func newTool(s *Server, t *mcpsdk.Tool, timeout time.Duration) *mcpTool {
	return &mcpTool{
		server:       s,
		name:         t.Name,
		inputSchema:  marshalSchema(t.InputSchema),
		outputSchema: marshalSchema(t.OutputSchema),
		timeout:      timeout,
	}
}

// marshalSchema renders an MCP tool schema as a raw JSON document. A nil
// schema stays empty, which the contract boundary reads as "accepts
// anything". Schemas arrive JSON-decoded over the wire, so re-marshaling
// them cannot fail.
func marshalSchema(schema any) json.RawMessage {
	if schema == nil {
		return nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	return raw
}

// Name returns the MCP tool name.
func (t *mcpTool) Name() string { return t.name }

// InputSchema returns the schema the server declared for the tool input.
func (t *mcpTool) InputSchema() json.RawMessage { return t.inputSchema }

// OutputSchema returns the schema the server declared for the tool result,
// or nil when the server declares none.
func (t *mcpTool) OutputSchema() json.RawMessage { return t.outputSchema }

// Call forwards the input as tools/call arguments under the per-call
// timeout and maps the result back: structured content when present,
// otherwise the content array. An MCP error result becomes an error
// carrying the server's text.
func (t *mcpTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t.server.dead.Load() {
		return nil, fmt.Errorf("call %q: %w: server %q exited", t.name, ErrServerUnavailable, t.server.name)
	}
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	res, err := t.server.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      t.name,
		Arguments: input,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("call %q on server %q: %w", t.name, t.server.name, ctx.Err())
		}
		return nil, fmt.Errorf("call %q on server %q: %w", t.name, t.server.name, err)
	}
	if res.IsError {
		return nil, fmt.Errorf("call %q on server %q: %s", t.name, t.server.name, contentText(res.Content))
	}
	if res.StructuredContent != nil {
		out, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return nil, fmt.Errorf("encode result of %q on server %q: %w", t.name, t.server.name, err)
		}
		return out, nil
	}
	out, err := json.Marshal(res.Content)
	if err != nil {
		return nil, fmt.Errorf("encode result of %q on server %q: %w", t.name, t.server.name, err)
	}
	return out, nil
}

// contentText joins the text blocks of an MCP content list for error
// reporting.
func contentText(content []mcpsdk.Content) string {
	var parts []string
	for _, c := range content {
		if text, ok := c.(*mcpsdk.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

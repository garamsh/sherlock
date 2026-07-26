package message

import (
	"context"
	"encoding/json"
	"fmt"
)

const sendInputSchema = `{
	"type": "object",
	"properties": {
		"channel": {"type": "string", "minLength": 1},
		"target": {"type": "string", "minLength": 1},
		"content": {"type": "string", "minLength": 1}
	},
	"required": ["channel", "target", "content"],
	"additionalProperties": false
}`

const sendOutputSchema = `{
	"type": "object",
	"properties": {
		"status": {"type": "string", "enum": ["queued"]}
	},
	"required": ["status"],
	"additionalProperties": false
}`

// SendToolName is the registered name of the message_send tool.
const SendToolName = "message_send"

// SendTool is the message_send tool: it validates the outbound message and
// hands it to the outbound sink. Delivery itself is the sink's concern.
type SendTool struct {
	sink Sink
}

// NewSendTool returns a message_send tool delivering through sink.
func NewSendTool(sink Sink) *SendTool {
	return &SendTool{sink: sink}
}

// Name returns the tool name.
func (t *SendTool) Name() string { return SendToolName }

// InputSchema returns the JSON Schema for the tool input.
func (t *SendTool) InputSchema() json.RawMessage { return json.RawMessage(sendInputSchema) }

// OutputSchema returns the JSON Schema for the tool result.
func (t *SendTool) OutputSchema() json.RawMessage { return json.RawMessage(sendOutputSchema) }

// Call hands the message to the sink and reports it as queued. Input has
// already passed schema validation at the contract boundary.
func (t *SendTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var msg Message
	if err := json.Unmarshal(input, &msg); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	if err := t.sink.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	return json.RawMessage(`{"status":"queued"}`), nil
}

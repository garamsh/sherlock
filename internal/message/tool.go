package message

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/garamsh/gagent/internal/tool"
)

// SendToolName is the registered name of the message_send tool.
const SendToolName = "message_send"

// sendToolDescription is the one-sentence summary advertised to model
// providers, matching the message responsibility in docs/architecture.
const sendToolDescription = "Queue an outbound message for delivery to a channel and target."

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

// Description returns the one-sentence tool summary.
func (t *SendTool) Description() string { return sendToolDescription }

// InputSchema returns the JSON Schema for the tool input.
func (t *SendTool) InputSchema() json.RawMessage { return InputSchema() }

// OutputSchema returns the JSON Schema for the tool result.
func (t *SendTool) OutputSchema() json.RawMessage { return OutputSchema() }

// CallOutput is the canonical typed form of the message_send tool's
// output schema; the schema/struct consistency test keeps them in
// lockstep. Message, the input type, is already exported from message.go.
type CallOutput struct {
	Status string `json:"status"`
}

// Call hands the message to the sink and reports it as queued. Input has
// already passed schema validation at the contract boundary.
func (t *SendTool) Call(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return tool.CallTyped(ctx, t.doCall, input)
}

func (t *SendTool) doCall(ctx context.Context, msg Message) (CallOutput, error) {
	if err := t.sink.Send(ctx, msg); err != nil {
		return CallOutput{}, fmt.Errorf("send message: %w", err)
	}
	return CallOutput{Status: "queued"}, nil
}

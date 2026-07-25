package message_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// recordingSink is a Sink fake that records every message.
type recordingSink struct {
	messages []message.Message
	err      error
}

func (s *recordingSink) Send(_ context.Context, msg message.Message) error {
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, msg)
	return nil
}

func TestSendTool_ForwardsToSink(t *testing.T) {
	sink := &recordingSink{}
	sendTool := message.NewSendTool(sink)

	out, err := tool.Invoke(context.Background(), sendTool,
		json.RawMessage(`{"channel":"slack","target":"#general","content":"hello world"}`))

	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"queued"}`, string(out))
	assert.Equal(t, []message.Message{
		{Channel: "slack", Target: "#general", Content: "hello world"},
	}, sink.messages)
}

func TestSendTool_InvalidInputRejectedAtBoundary(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "missing channel", input: `{"target":"#general","content":"hi"}`},
		{name: "missing target", input: `{"channel":"slack","content":"hi"}`},
		{name: "missing content", input: `{"channel":"slack","target":"#general"}`},
		{name: "empty channel", input: `{"channel":"","target":"#general","content":"hi"}`},
		{name: "empty content", input: `{"channel":"slack","target":"#general","content":""}`},
		{name: "additional field", input: `{"channel":"slack","target":"#general","content":"hi","priority":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			sendTool := message.NewSendTool(sink)

			_, err := tool.Invoke(context.Background(), sendTool, json.RawMessage(tt.input))

			require.Error(t, err)
			assert.ErrorIs(t, err, tool.ErrValidation)
			assert.Empty(t, sink.messages, "sink must not be called on invalid input")
		})
	}
}

func TestSendTool_SinkErrorPropagates(t *testing.T) {
	sink := &recordingSink{err: errors.New("gateway unavailable")}
	sendTool := message.NewSendTool(sink)

	_, err := tool.Invoke(context.Background(), sendTool,
		json.RawMessage(`{"channel":"slack","target":"#general","content":"hi"}`))

	require.Error(t, err)
	assert.ErrorIs(t, err, tool.ErrExecution)
	assert.ErrorContains(t, err, "gateway unavailable")
}

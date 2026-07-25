package gateway_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
)

func TestOutbox_SendWritesJSONFile(t *testing.T) {
	envDir := t.TempDir()
	outbox := gateway.NewOutbox(envDir)

	msg := message.Message{Channel: "http", Target: "sender", Content: "hello back"}
	require.NoError(t, outbox.Send(context.Background(), msg))
	require.NoError(t, outbox.Send(context.Background(), msg))

	entries, err := os.ReadDir(filepath.Join(envDir, gateway.OutboxDir))
	require.NoError(t, err)
	require.Len(t, entries, 2)
	// Delivery order is lexicographic.
	assert.Less(t, entries[0].Name(), entries[1].Name())

	raw, err := os.ReadFile(filepath.Join(envDir, gateway.OutboxDir, entries[0].Name()))
	require.NoError(t, err)
	var got message.Message
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, msg, got)
}

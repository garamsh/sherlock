package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// deliveringModel stands in for a turn whose message_send call lands
// while the turn is running: it delivers on its first Chat, which is
// after Turn.Run has taken its snapshot, then answers.
type deliveringModel struct {
	t         *testing.T
	dir, name string
	msg       message.Message
	answer    string
	delivered bool
}

func (m *deliveringModel) Chat(context.Context, []agent.Message, []tool.Descriptor) (agent.Response, error) {
	if !m.delivered {
		deliver(m.t, m.dir, m.name, m.msg)
		m.delivered = true
	}
	return agent.Response{Content: m.answer}, nil
}

// deliver writes one message into dir the way the message_send tool
// does: a JSON file whose name orders it. The turn reads what the tool
// wrote, so the test writes what the tool writes.
func deliver(t *testing.T, dir, name string, msg message.Message) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), body, 0o600))
}

// turnFactory returns a Factory whose model runs the given script
// against no tools, with memory in a tmpdir.
func turnFactory(t *testing.T, steps ...agent.ScriptedStep) (*gateway.Factory, string) {
	t.Helper()
	memoryPath := filepath.Join(t.TempDir(), "memory.db")
	return &gateway.Factory{
		MemoryPath: memoryPath,
		NewModel:   func(string) agent.Model { return agent.NewScriptedModel(steps...) },
	}, memoryPath
}

// TestTurn_EchoesResponseAndDeliveredMessages holds the one-shot
// contract: a caller with no outbox watcher sees the response and
// everything the turn delivered, on the writer it supplied.
func TestTurn_EchoesResponseAndDeliveredMessages(t *testing.T) {
	memoryPath := filepath.Join(t.TempDir(), "memory.db")
	outboxDir := gateway.DirFor(memoryPath)

	// One message predates the turn and one lands during it; only the
	// second is this turn's to report.
	deliver(t, outboxDir, "0-earlier.json", message.Message{
		Channel: "http", Target: "someone", Content: "from an earlier turn"})
	model := &deliveringModel{
		t: t, dir: outboxDir, name: "1-during.json",
		msg:    message.Message{Channel: "http", Target: "sender", Content: "42"},
		answer: "done",
	}
	factory := &gateway.Factory{
		MemoryPath: memoryPath,
		NewModel:   func(string) agent.Model { return model },
	}

	var out bytes.Buffer
	require.NoError(t, gateway.Turn{
		Factory: factory, AgentID: "a1", OutboxDir: outboxDir,
		Out: &out, Logger: slog.New(slog.DiscardHandler),
	}.Run(context.Background(), "hello"))

	assert.Contains(t, out.String(), "done", "the turn's response reaches the writer")
	assert.Contains(t, out.String(), "[http -> sender] 42")
	assert.NotContains(t, out.String(), "from an earlier turn",
		"only messages this turn delivered are echoed")
}

// TestTurn_ReportsFailedToolCalls holds the exit-status contract: a
// failed tool call does not end the turn, so a clean return would tell
// a caller who stopped watching that the run was clean when it was not.
func TestTurn_ReportsFailedToolCalls(t *testing.T) {
	factory, memoryPath := turnFactory(t,
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name: "absent_tool", Input: json.RawMessage(`{}`)}}},
		agent.ScriptedStep{Content: "carried on"},
	)

	var out bytes.Buffer
	err := gateway.Turn{
		Factory: factory, AgentID: "a1", OutboxDir: gateway.DirFor(memoryPath),
		Out: &out, Logger: slog.New(slog.DiscardHandler),
	}.Run(context.Background(), "hello")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "absent_tool", "the error names what failed")
	assert.Contains(t, out.String(), "carried on",
		"the turn's output is written before the failure is reported")
}

// TestTurn_MissingOutboxIsNotAFailure covers the turn that delivers
// nothing: the directory the message_send tool would create does not
// exist yet, and that is an ordinary clean run.
func TestTurn_MissingOutboxIsNotAFailure(t *testing.T) {
	factory, memoryPath := turnFactory(t, agent.ScriptedStep{Content: "nothing to send"})

	var out bytes.Buffer
	err := gateway.Turn{
		Factory: factory, AgentID: "a1", OutboxDir: gateway.DirFor(memoryPath),
		Out: &out, Logger: slog.New(slog.DiscardHandler),
	}.Run(context.Background(), "hello")

	require.NoError(t, err)
	assert.Contains(t, out.String(), "nothing to send")
}

// TestDirFor pins the one definition of the outbox path. It is computed
// here and nowhere else: the value reaches the message_send tool as
// GAGENT_OUTBOX_DIR and this package reads back what the tool wrote
// there, so writer and reader agree by naming one function rather than
// by both spelling the same join.
func TestDirFor(t *testing.T) {
	assert.Equal(t, filepath.Join("data", "outbox"), gateway.DirFor("data/memory.db"))
	assert.Equal(t, filepath.Join("data", gateway.OutboxDir), gateway.DirFor("data/memory.db"))
}

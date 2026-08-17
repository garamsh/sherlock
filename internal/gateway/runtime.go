package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/tool"
)

// Runtime is everything the agent needs to process queued messages: its
// rolling agent session plus a release hook for the resources the
// session holds (the memory store).
type Runtime struct {
	Session agent.Session
	// Tools is the platform tool client the session drives; nil means
	// an empty tool surface (tests).
	Tools agent.ToolClient
	// Close releases the runtime's resources. Called once on queue
	// shutdown.
	Close func(ctx context.Context) error
	// Retain is the tiered retention pass (ADR 0006) and the only pass
	// the runner evaluates after a completed turn; a failure logs at
	// WARN and never fails a turn. Nil means no retention pass (test
	// runtimes).
	Retain func(ctx context.Context) error
}

// Factory assembles the one agent's runtime from the agent-container
// modules (ADR 0017): the SQLite memory store at MemoryPath, the
// platform tool client (the per-operator tool list), and the rolling
// session. It wires no outbound delivery, because there is none to
// wire: the message_send binary the platform client spawns writes each
// message as a JSON file into the outbox directory it is handed, and
// that write is the delivery (ADR 0007).
type Factory struct {
	// MemoryPath is the agent's SQLite memory file, created on first
	// use.
	MemoryPath string
	// NewModel returns the chat model for the agent's session.
	NewModel func(agentID string) agent.Model
	// Ego is the session ego; empty selects the agent package's embedded
	// default (the platform contract is appended regardless).
	Ego string
	// MaxHistoryMessages overrides agentmemory.DefaultMaxHistoryMessages.
	// Zero selects that default; a negative value is clamped to zero,
	// which disables context compaction.
	MaxHistoryMessages int
	// Tools is the platform tool client. Nil means the agent runs
	// without tools (tests).
	Tools agent.ToolClient
}

// New builds the agent runtime: it opens (creating on first use) the
// memory store and the session, then wires the session's recall, writeback,
// event, and context-compaction hooks.
func (f *Factory) New(_ context.Context, agentID string, events agent.EventSink) (*Runtime, error) {
	if err := os.MkdirAll(filepath.Dir(f.MemoryPath), 0o755); err != nil {
		return nil, err
	}
	store, err := sqlite.Open(f.MemoryPath)
	if err != nil {
		return nil, err
	}
	maxHistoryMessages := f.MaxHistoryMessages
	if maxHistoryMessages == 0 {
		maxHistoryMessages = agentmemory.DefaultMaxHistoryMessages
	}
	if maxHistoryMessages < 0 {
		maxHistoryMessages = 0
	}
	memSvc := memory.NewService(store)

	tools := f.Tools
	if tools == nil {
		tools = nullClient{}
	}

	session := agent.NewSession(f.NewModel(agentID), tools, agent.Options{
		// No Recaller: asked in words an addressed store answers
		// nothing at all (ADR 0018), and the step that reads a
		// question into an address is #336 step 3. A session without
		// one states the emptiness rather than injecting anything.
		Writeback:          agentmemory.Writeback{Svc: memSvc},
		Events:             events,
		Compactor:          agentmemory.Compactor{MaxHistoryMessages: maxHistoryMessages},
		Ego:                f.Ego,
		MaxHistoryMessages: maxHistoryMessages,
	})
	return &Runtime{
		Session: session,
		Tools:   tools,
		Retain: func(ctx context.Context) error {
			_, err := memSvc.Retain(ctx)
			return err
		},
		Close: func(context.Context) error {
			// The store opens in WAL mode, so an unclosed handle
			// leaves the `-wal` and `-shm` files beside `memory.db`
			// with the last checkpoint unrun, and that file is what
			// the next agent starts from (`deployment.md`).
			return memSvc.Close()
		},
	}, nil
}

// nullClient is the no-tool tool client used by test runtimes that
// only exercise the reasoning loop. List returns an empty slice; Call
// always fails with an unknown-tool error.
type nullClient struct{}

// List implements agent.ToolClient.
func (nullClient) List() []tool.Descriptor { return nil }

// Call implements agent.ToolClient.
func (nullClient) Call(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("agent: no tool client configured: " + name)
}

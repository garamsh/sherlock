package gateway

import (
	"context"
	"os"
	"path/filepath"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// Runtime is everything the agent needs to process queued messages: its
// rolling agent session plus a release hook for the resources the
// session holds (the memory store).
type Runtime struct {
	Session agent.Session
	// Close releases the runtime's resources. Called once on queue
	// shutdown.
	Close func(ctx context.Context) error
	// Retain is an optional legacy maintenance hook; the source-linked
	// runtime leaves it nil because source records are not evicted.
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
	// Tools is the platform tool client. Required: the session calls it
	// on every turn.
	Tools agent.ToolClient
}

// New builds the agent runtime: it opens (creating on first use) the
// memory store and the session, then wires source-linked recall, inspection,
// event, and context-compaction hooks.
func (f *Factory) New(_ context.Context, agentID string, events agent.EventSink) (*Runtime, error) {
	if err := os.MkdirAll(filepath.Dir(f.MemoryPath), 0o755); err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(f.MemoryPath)
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
	recent, err := store.Recent(context.Background(), maxHistoryMessages)
	if err != nil {
		store.Close()
		return nil, err
	}
	initial := make([]agent.Message, 0, len(recent))
	for _, m := range recent {
		initial = append(initial, agent.Message{Role: m.Role, Content: m.Content})
	}

	// Search wording and the reasoning loop use the same configured model.
	model := f.NewModel(agentID)

	session := agent.NewSession(model, f.Tools, agent.Options{
		Recaller:           agentmemory.ConversationRecall{Store: store, Model: model},
		Conversation:       store,
		InitialHistory:     initial,
		Events:             events,
		Compactor:          agentmemory.Compactor{MaxHistoryMessages: maxHistoryMessages},
		Ego:                f.Ego,
		MaxHistoryMessages: maxHistoryMessages,
	})
	return &Runtime{
		Session: session,
		Close: func(context.Context) error {
			// The store opens in WAL mode, so an unclosed handle
			// leaves the `-wal` and `-shm` files beside `memory.db`
			// with the last checkpoint unrun, and that file is what
			// the next agent starts from (`deployment.md`).
			return store.Close()
		},
	}, nil
}

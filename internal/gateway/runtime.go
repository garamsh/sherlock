package gateway

import (
	"context"
	"os"
	"path/filepath"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// Runtime is everything the agent needs to process queued messages: its
// rolling agent session plus a release hook for the resources the
// session holds (the memory store).
type Runtime struct {
	Session agent.Session
	// Close releases the runtime's resources. Called once on queue
	// shutdown.
	Close func(ctx context.Context) error
	// Retain is the optional pass the runner makes after each completed
	// turn, off the turn path. The source-linked runtime names the entities
	// of one mid episode stored before entities were (memory.md).
	Retain func(ctx context.Context) error
	// Committed reports whether memory holds the message ID within the
	// scope of issuer and sender and, when it does, whether with content.
	// It is the only evidence a message completed; nil means the runtime
	// commits nothing.
	Committed func(ctx context.Context, issuer, sender, id, content string) (held, same bool, err error)
	// Posted reports whether memory holds a message the transport
	// principal poster posted under id, signed or not. Nil means the
	// runtime commits nothing.
	Posted func(ctx context.Context, poster, id string) (bool, error)
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
	// use unless RequireStore is set.
	MemoryPath string
	// RequireStore refuses a memory file that is absent or uninitialized
	// instead of creating it, and creates nothing on the way (ADR 0025).
	RequireStore bool
	// NewModel returns the chat model for the agent's session.
	NewModel func(agentID string) agent.Model
	// Embedder ranks mid episodes by similarity for recall.
	Embedder agent.Embedder
	// Ego is the session ego; empty selects the agent package's embedded
	// default (the platform contract is appended regardless).
	Ego string
	// OperatorInstructions is the operator's deployment text, placed
	// between the ego and the platform contract; empty omits it.
	OperatorInstructions string
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
func (f *Factory) New(ctx context.Context, agentID string, events agent.EventSink) (*Runtime, error) {
	store, err := f.openStore()
	if err != nil {
		return nil, err
	}
	model := f.NewModel(agentID)
	summarizer := agentmemory.ConversationSummary{Model: model}
	if err := store.Accumulate(ctx, summarizer); err != nil {
		store.Close()
		return nil, err
	}
	maxHistoryMessages := f.MaxHistoryMessages
	if maxHistoryMessages == 0 {
		maxHistoryMessages = agentmemory.DefaultMaxHistoryMessages
	}
	if maxHistoryMessages < 0 {
		maxHistoryMessages = 0
	}
	restoreLimit := maxHistoryMessages
	if f.MaxHistoryMessages < 0 {
		// Disabled compaction also restores the full visible conversation.
		restoreLimit = -1
	}
	recent, err := store.Recent(ctx, restoreLimit)
	if err != nil {
		store.Close()
		return nil, err
	}
	initial := make([]agent.Message, 0, len(recent))
	for _, m := range recent {
		initial = append(initial, agent.Message{Role: m.Role, Content: m.Content})
	}
	session := agent.NewSession(model, f.Tools, agent.Options{
		Recaller:             agentmemory.ConversationRecall{Store: store},
		Conversation:         store,
		Summarizer:           summarizer,
		InitialHistory:       initial,
		Events:               events,
		Compactor:            agentmemory.Compactor{MaxHistoryMessages: maxHistoryMessages},
		Ego:                  f.Ego,
		OperatorInstructions: f.OperatorInstructions,
		MaxHistoryMessages:   maxHistoryMessages,
	})
	return &Runtime{
		Session: session,
		Retain: func(ctx context.Context) error {
			return store.Link(ctx, summarizer)
		},
		Committed: store.HoldsInbound,
		Posted:    store.PostedInbound,
		Close: func(context.Context) error {
			// The store opens in WAL mode, so an unclosed handle
			// leaves the `-wal` and `-shm` files beside `memory.db`
			// with the last checkpoint unrun, and that file is what
			// the next agent starts from (`deployment.md`).
			return store.Close()
		},
	}, nil
}

// openStore opens the memory store at MemoryPath, creating it and its
// directory on first use unless RequireStore is set.
func (f *Factory) openStore() (*sqlite.Store, error) {
	if f.RequireStore {
		return sqlite.OpenInitializedConversation(f.MemoryPath, f.Embedder)
	}
	if err := os.MkdirAll(filepath.Dir(f.MemoryPath), 0o755); err != nil {
		return nil, err
	}
	return sqlite.OpenConversation(f.MemoryPath, f.Embedder)
}

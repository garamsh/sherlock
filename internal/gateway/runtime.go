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
	// shutdown; memory consolidation (ADR 0011) runs before the store
	// closes.
	Close func(ctx context.Context) error
	// ConsolidateIfDue is the threshold consolidation trigger (ADR 0011):
	// the runner evaluates it after a completed turn, off the turn path;
	// a failure logs at WARN and never fails a turn. Nil means no
	// threshold trigger (test runtimes).
	ConsolidateIfDue func(ctx context.Context) error
	// Retain is the tiered retention pass (ADR 0006): the runner
	// evaluates it after a completed turn, alongside ConsolidateIfDue;
	// a failure logs at WARN and never fails a turn. Nil means no
	// retention pass (test runtimes).
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
	// MaxHistoryMessages overrides the compaction package default. A negative
	// value disables context compaction.
	MaxHistoryMessages int
	// Summarizer overrides the deterministic context summarizer. Nil uses
	// memory.NewConcatSummarizer.
	Summarizer memory.Summarizer
	// Embedder overrides the memory embedder. Nil uses
	// memory.NewHashingEmbedder, the deterministic offline default.
	Embedder memory.Embedder
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
	summarizer := f.Summarizer
	if summarizer == nil {
		summarizer = memory.NewConcatSummarizer()
	}
	maxHistoryMessages := f.MaxHistoryMessages
	if maxHistoryMessages == 0 {
		maxHistoryMessages = agentmemory.DefaultMaxHistoryMessages
	}
	if maxHistoryMessages < 0 {
		maxHistoryMessages = 0
	}
	embedder := f.Embedder
	if embedder == nil {
		embedder = memory.NewHashingEmbedder()
	}
	memSvc := memory.NewService(store, embedder, summarizer)

	tools := f.Tools
	if tools == nil {
		tools = nullClient{}
	}

	session := agent.NewSession(f.NewModel(agentID), tools, agent.Options{
		Recaller:           agentmemory.Recall{Svc: memSvc},
		Writeback:          agentmemory.Writeback{Svc: memSvc},
		Events:             events,
		Compactor:          agentmemory.Compactor{Svc: memSvc, Summarizer: summarizer, MaxHistoryMessages: maxHistoryMessages},
		Ego:                f.Ego,
		MaxHistoryMessages: maxHistoryMessages,
	})
	return &Runtime{
		Session: session,
		Tools:   tools,
		ConsolidateIfDue: func(ctx context.Context) error {
			_, err := memSvc.ConsolidateIfDue(ctx)
			return err
		},
		Retain: func(ctx context.Context) error {
			_, err := memSvc.Retain(ctx)
			return err
		},
		Close: func(ctx context.Context) error {
			// Consolidation writes, so it runs before the handle
			// closes. The close runs even when it fails: the store
			// opens in WAL mode, so an unclosed handle leaves the
			// `-wal` and `-shm` files beside `memory.db` with the
			// last checkpoint unrun, and that file is what the next
			// agent starts from (`deployment.md`).
			_, err := memSvc.Consolidate(ctx)
			return errors.Join(err, memSvc.Close())
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

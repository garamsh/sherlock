package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
)

// Runtime is everything the agent needs to process queued messages: its
// rolling agent session plus a release hook for the resources the
// session holds (the memory store).
type Runtime struct {
	Session agent.Session
	// Registry is the session's tool registry, exposed for tests and
	// observability. Production callers go through Session.Run.
	Registry *tool.Registry
	// Capture stores one inbound message verbatim as an episodic
	// user-turn memory (ADR 0012). The ingress path calls it at accept
	// time, before the message is queued.
	Capture func(ctx context.Context, content string) error
	// Close releases the runtime's resources. Called once on queue
	// shutdown; memory consolidation (ADR 0011) runs before the store
	// closes.
	Close func(ctx context.Context) error
	// ConsolidateIfDue is the threshold consolidation trigger (ADR 0011):
	// the runner evaluates it after a completed turn, off the turn path;
	// a failure logs at WARN and never fails a turn. Nil means no
	// threshold trigger (test runtimes).
	ConsolidateIfDue func(ctx context.Context) error
}

// Factory assembles the one agent's runtime from the agent-container
// modules (ADR 0018): the SQLite memory store at MemoryPath, the mouth
// (message_send, in-process), and every tool discovered from the
// configured tool servers. The agent holds no built-in computer tools —
// files, shell_exec, and script tools live in the sidecar and join the
// registry as remote tools through the tool contract.
type Factory struct {
	// MemoryPath is the agent's SQLite memory file, created on first
	// use.
	MemoryPath string
	// NewModel returns the chat model for the agent's session.
	NewModel func(agentID string) agent.Model
	// Ego is the session ego; empty selects the agent package's embedded
	// default (the platform contract is appended regardless).
	Ego string
	// MaxHistoryMessages overrides DefaultMaxHistoryMessages. A negative
	// value disables context compaction.
	MaxHistoryMessages int
	// Summarizer overrides the deterministic context summarizer. Nil uses
	// memory.NewConcatSummarizer.
	Summarizer memory.Summarizer
	// Embedder overrides the memory embedder. Nil uses
	// memory.NewHashingEmbedder, the deterministic offline default.
	Embedder memory.Embedder
	// Sink, when non-nil, receives outbound messages instead of the
	// default file outbox (the one-shot agent CLI prints them to stdout).
	Sink message.Sink
	// NewSink, when non-nil and Sink is nil, builds the agent's outbound
	// sink around the default file outbox (the console sink uses the
	// outbox as its delivery-failure fallback). Nil selects the file
	// outbox itself.
	NewSink func(agentID string, fallback *Outbox) message.Sink
	// Tools lists the discovered remote tools registered alongside
	// message_send. A name colliding with message_send fails New with
	// tool.ErrDuplicateTool.
	Tools []tool.Tool
}

// New builds the agent runtime: it opens (creating on first use) the
// memory store and tool set, then wires the session's recall, writeback,
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
		maxHistoryMessages = DefaultMaxHistoryMessages
	}
	if maxHistoryMessages < 0 {
		maxHistoryMessages = 0
	}
	embedder := f.Embedder
	if embedder == nil {
		embedder = memory.NewHashingEmbedder()
	}
	memSvc := memory.NewService(store, embedder, summarizer)

	sink := f.Sink
	if sink == nil {
		outbox := NewOutbox(filepath.Dir(f.MemoryPath))
		sink = outbox
		if f.NewSink != nil {
			sink = f.NewSink(agentID, outbox)
		}
	}

	registry := tool.NewRegistry()
	tools := append([]tool.Tool{message.NewSendTool(sink)}, f.Tools...)
	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return nil, errors.Join(err, store.Close())
		}
	}

	session := agent.NewSession(f.NewModel(agentID), registry, agent.Options{
		Recaller:           Recall{Svc: memSvc},
		Writeback:          Writeback{Svc: memSvc},
		Events:             events,
		Compactor:          Compactor{Svc: memSvc, Summarizer: summarizer, MaxHistoryMessages: maxHistoryMessages},
		Ego:                f.Ego,
		MaxHistoryMessages: maxHistoryMessages,
	})
	return &Runtime{
		Session:  session,
		Registry: registry,
		Capture:  Capture{Svc: memSvc}.Capture,
		ConsolidateIfDue: func(ctx context.Context) error {
			_, err := memSvc.ConsolidateIfDue(ctx)
			return err
		},
		Close: func(ctx context.Context) error {
			_, err := memSvc.Consolidate(ctx)
			return errors.Join(err, memSvc.Close())
		},
	}, nil
}

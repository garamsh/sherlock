package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// MemoryDBFile is the memory database filename inside the environment
// directory, per docs/architecture/environment.md.
const MemoryDBFile = "memory.db"

// DefaultShellExecTimeout is the per-call ceiling applied to shell_exec
// sub-processes when a Factory leaves ShellExecTimeout at its zero value
// (ADR 0012).
const DefaultShellExecTimeout = 30 * time.Second

// Runtime is everything one environment needs to process queued messages:
// its rolling agent session plus a release hook for the resources the
// session's tools hold (the memory store).
type Runtime struct {
	Session agent.Session
	// Registry is the session's tool registry, exposed for tests and
	// observability. Production callers go through Session.Run.
	Registry *tool.Registry
	// Close releases the runtime's resources. Called once on router
	// shutdown.
	Close func(ctx context.Context) error
}

// Factory assembles an environment runtime from the platform modules:
// environment layout, SQLite memory, and the four built-in tools
// (memory, files, message_send, shell_exec) wired into one registry.
// The model is supplied per environment through NewModel so each rolling
// session gets its own model instance.
//
// ADR 0012 restores the shell_exec tool: an agent-supplied command runs
// as a sub-process dropped to the agent UID via NewShellExecutor. A nil
// NewShellExecutor leaves shell_exec out of the registry — the agent
// then has the three typed tools only.
type Factory struct {
	// Root is the parent directory of all environment data
	// (environment.DefaultRoot by default).
	Root string
	// NewModel returns the chat model for one environment's session.
	NewModel func(envID string) agent.Model
	// Ego is the session ego for every environment; empty selects
	// the agent package's embedded default (the platform contract is
	// appended regardless).
	Ego string
	// MaxHistoryMessages overrides DefaultMaxHistoryMessages for every
	// environment session. A negative value disables context compaction.
	MaxHistoryMessages int
	// Summarizer overrides the deterministic context summarizer. Nil uses
	// memory.NewConcatSummarizer.
	Summarizer memory.Summarizer
	// Sink, when non-nil, receives outbound messages instead of the default
	// file outbox (the one-shot agent CLI prints them to stdout).
	Sink message.Sink
	// ShellExecTimeout caps one shell_exec sub-process run. A non-positive
	// value selects DefaultShellExecTimeout.
	ShellExecTimeout time.Duration
	// NewShellExecutor constructs the Executor that backs the shell_exec
	// tool. Nil leaves shell_exec unregistered.
	NewShellExecutor func() shell.Executor
}

var _ RuntimeFactory = (*Factory)(nil)

// New implements RuntimeFactory: it opens (creating on first use) the
// environment directory, memory store, and tool set, then wires the
// session's recall, writeback, event, and context-compaction hooks.
func (f *Factory) New(_ context.Context, envID string, events agent.EventSink) (*Runtime, error) {
	env, err := environment.Open(f.Root, envID)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.Open(filepath.Join(env.Dir(), MemoryDBFile))
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
	memSvc := memory.NewService(store, memory.NewHashingEmbedder(), summarizer)
	fs, err := files.Open(env.Workspace())
	if err != nil {
		return nil, errors.Join(err, store.Close())
	}

	sink := f.Sink
	if sink == nil {
		sink = NewOutbox(env.Dir())
	}

	shellTimeout := f.ShellExecTimeout
	if shellTimeout <= 0 {
		shellTimeout = DefaultShellExecTimeout
	}

	registry := tool.NewRegistry()
	for _, t := range []tool.Tool{
		memory.NewTool(memSvc),
		files.NewTool(fs),
		message.NewSendTool(sink),
	} {
		if err := registry.Register(t); err != nil {
			return nil, errors.Join(err, store.Close())
		}
	}
	if f.NewShellExecutor != nil {
		shellTool := shell.NewTool(f.NewShellExecutor()).WithDefaultTimeout(shellTimeout)
		if err := registry.Register(shellTool); err != nil {
			return nil, errors.Join(err, store.Close())
		}
	}

	session := agent.NewSession(f.NewModel(envID), registry, agent.Options{
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
		Close: func(ctx context.Context) error {
			return memSvc.Close()
		},
	}, nil
}

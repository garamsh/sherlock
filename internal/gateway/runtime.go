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
	"github.com/garamsh/gagent/internal/sandbox"
	"github.com/garamsh/gagent/internal/tool"
)

// MemoryDBFile is the memory database filename inside the environment
// directory, per docs/architecture/environment.md.
const MemoryDBFile = "memory.db"

// Runtime is everything one environment needs to process queued messages:
// its rolling agent session plus a release hook for the resources the
// session's tools hold (memory store, environment container).
type Runtime struct {
	Session agent.Session
	// Close releases the runtime's resources. Called once on router
	// shutdown.
	Close func(ctx context.Context) error
}

// Factory assembles an environment runtime from the platform modules:
// environment layout, SQLite memory, and the memory/files/sandbox/message
// tools wired into one registry (issue #8 wiring). The model is supplied
// per environment through NewModel so each rolling session gets its own
// model instance.
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
	// Sandbox runs environment containers for the sandbox_exec tool.
	Sandbox sandbox.Runtime
	// Image is the environment container image reference.
	Image string
	// Policy bounds the environment container's resources.
	Policy sandbox.Policy
	// ExecTimeout bounds every sandbox_exec command.
	ExecTimeout time.Duration
	// Sink, when non-nil, receives outbound messages instead of the default
	// file outbox (the one-shot agent CLI prints them to stdout).
	Sink message.Sink
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
	exec := sandbox.NewExecTool(f.Sandbox, sandbox.Config{
		EnvID:     envID,
		Workspace: env.Workspace(),
		Image:     f.Image,
		Policy:    f.Policy,
	}, f.ExecTimeout)

	sink := f.Sink
	if sink == nil {
		sink = NewOutbox(env.Dir())
	}

	registry := tool.NewRegistry()
	for _, t := range []tool.Tool{
		memory.NewTool(memSvc),
		files.NewTool(fs),
		exec,
		message.NewSendTool(sink),
	} {
		if err := registry.Register(t); err != nil {
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
		Session: session,
		Close: func(ctx context.Context) error {
			return errors.Join(memSvc.Close(), exec.Destroy(ctx))
		},
	}, nil
}

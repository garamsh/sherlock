package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"github.com/garamsh/gagent/internal/tool/script"
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
	// Capture stores one inbound message verbatim as an episodic
	// user-turn memory (ADR 0016). The ingress path calls it at accept
	// time, before the message is queued.
	Capture func(ctx context.Context, content string) error
	// Close releases the runtime's resources. Called once on router
	// shutdown; memory consolidation (ADR 0015) runs before the store
	// closes.
	Close func(ctx context.Context) error
}

// Factory assembles an environment runtime from the platform modules:
// environment layout, SQLite memory, and the four built-in tools
// (memory, files, message_send, shell_exec) wired into one registry,
// plus any script tools the platform config declares.
// The model is supplied per environment through NewModel so each rolling
// session gets its own model instance.
//
// ADR 0012 restores the shell_exec tool: an agent-supplied command runs
// as a sub-process dropped to the agent UID via NewShellExecutor. A nil
// NewShellExecutor leaves shell_exec out of the registry — the agent
// then has the three typed tools only. ADR 0014 routes script tools
// through the same executor: they drop to the agent UID too, and with
// no executor wired their calls fail closed with agent_uid unavailable
// instead of running as the platform UID.
type Factory struct {
	// Root is the parent directory of all environment data
	// (environment.DefaultRoot by default).
	Root string
	// NewModel returns the chat model for one agent's session.
	NewModel func(agentID string) agent.Model
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
	// Embedder overrides the memory embedder. Nil uses
	// memory.NewHashingEmbedder, the deterministic offline default.
	Embedder memory.Embedder
	// Sink, when non-nil, receives outbound messages instead of the default
	// file outbox (the one-shot agent CLI prints them to stdout).
	Sink message.Sink
	// NewSink, when non-nil and Sink is nil, builds each agent's
	// outbound sink around the default file outbox (the console sink uses
	// the outbox as its delivery-failure fallback). Nil selects the file
	// outbox itself.
	NewSink func(agentID string, fallback *Outbox) message.Sink
	// ShellExecTimeout caps one shell_exec sub-process run. A non-positive
	// value selects DefaultShellExecTimeout.
	ShellExecTimeout time.Duration
	// NewShellExecutor constructs the Executor that backs the shell_exec
	// tool and every script tool (ADR 0014: script tools drop to the
	// agent UID through the same executor). Nil leaves shell_exec
	// unregistered; script tools still register but their calls fail
	// closed with agent_uid unavailable.
	NewShellExecutor func() shell.Executor
	// Tools lists the script tools from the platform config, registered
	// alongside the built-ins. Platform entry points validate the section
	// at startup through ValidateTools: a name colliding with a built-in
	// fails startup with tool.ErrDuplicateTool.
	Tools []script.Config
}

var _ RuntimeFactory = (*Factory)(nil)

// ValidateTools checks the platform config's tools: section the way New
// would, without opening any environment. Platform entry points call it
// at startup — the boundary for platform config
// (docs/convention/runtime-safety.md §Boundaries) — so an invalid tools:
// entry fails fast with an error naming the offending tool instead of
// surfacing on the first message.
func (f *Factory) ValidateTools() error {
	return script.ValidateConfigs(f.Tools)
}

// secureMemoryDB enforces the memory-UID trust domain (ADR 0006): a
// privileged platform (euid 0) chowns memory.db to shell.DefaultMemoryUID
// with mode 0600, so agent-UID children get EACCES from the kernel while
// the memory service keeps opening the file as the platform process. An
// unprivileged platform owns the file itself and skips the chown.
func secureMemoryDB(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	if err := os.Chown(path, shell.DefaultMemoryUID, -1); err != nil {
		return fmt.Errorf("chown %q to memory uid: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %q: %w", path, err)
	}
	return nil
}

// New implements RuntimeFactory: it opens (creating on first use) the
// environment directory, memory store, and tool set, then wires the
// session's recall, writeback, event, and context-compaction hooks.
func (f *Factory) New(_ context.Context, agentID string, events agent.EventSink) (*Runtime, error) {
	env, err := environment.Open(f.Root, agentID)
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(env.Dir(), MemoryDBFile)
	store, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := secureMemoryDB(dbPath); err != nil {
		return nil, errors.Join(err, store.Close())
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
	if err := files.SecureWorkspace(env.Workspace(), shell.DefaultAgentUID); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	memSvc := memory.NewService(store, embedder, summarizer)
	fs, err := files.Open(env.Workspace(), shell.DefaultAgentUID)
	if err != nil {
		return nil, errors.Join(err, store.Close())
	}

	sink := f.Sink
	if sink == nil {
		outbox := NewOutbox(env.Dir())
		sink = outbox
		if f.NewSink != nil {
			sink = f.NewSink(agentID, outbox)
		}
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
		shellTool := shell.NewTool(f.NewShellExecutor()).
			WithDefaultTimeout(shellTimeout).
			WithWorkdir(env.Workspace())
		if err := registry.Register(shellTool); err != nil {
			return nil, errors.Join(err, store.Close())
		}
	}
	scriptExec := shell.UnavailableExecutor()
	if f.NewShellExecutor != nil {
		scriptExec = f.NewShellExecutor()
	}
	for _, st := range f.Tools {
		spec, err := st.Spec()
		if err != nil {
			return nil, errors.Join(err, store.Close())
		}
		if err := registry.Register(script.New(spec, scriptExec)); err != nil {
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
		Close: func(ctx context.Context) error {
			_, err := memSvc.Consolidate(ctx)
			return errors.Join(err, memSvc.Close())
		},
	}, nil
}

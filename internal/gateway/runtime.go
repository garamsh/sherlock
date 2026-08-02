package gateway

import (
	"context"
	"encoding/json"
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
	// Embedder overrides the memory embedder. Nil uses
	// memory.NewHashingEmbedder, the deterministic offline default.
	Embedder memory.Embedder
	// Sink, when non-nil, receives outbound messages instead of the default
	// file outbox (the one-shot agent CLI prints them to stdout).
	Sink message.Sink
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
	Tools []ScriptTool
}

var _ RuntimeFactory = (*Factory)(nil)

// ScriptTool is one entry of the platform config's tools: section — the
// single shape a script tool has from the config file to the gateway
// (docs/convention/simplicity.md: one concept, one shape). The
// mapstructure tags are how viper decodes the entry; spec reads the
// schema files and builds the script.Spec at registration. Adding a
// script tool is a config change, not a code change.
type ScriptTool struct {
	// Name is the tool name; it must not collide with a built-in.
	Name string `mapstructure:"name"`
	// Description is the one-sentence tool summary shown to the model.
	Description string `mapstructure:"description"`
	// InputSchema is the path to the input JSON Schema file.
	InputSchema string `mapstructure:"input_schema"`
	// OutputSchema is the path to the output JSON Schema file.
	OutputSchema string `mapstructure:"output_schema"`
	// Exec describes how to run the tool.
	Exec ScriptToolExec `mapstructure:"exec"`
}

// ScriptToolExec describes how to run a script tool with the timeout
// still in seconds; spec converts it to time.Duration when building the
// script.Spec. TimeoutSeconds is an explicit integer: unmarshalling a
// config integer straight into time.Duration would yield nanoseconds.
type ScriptToolExec struct {
	// Command is the argv array (first element is the executable).
	Command []string `mapstructure:"command"`
	// StdinFormat selects how input is fed to the process ("json").
	StdinFormat string `mapstructure:"stdin_format"`
	// StdoutFormat selects how stdout is interpreted ("json" validates
	// and returns a JSON document; anything else is returned as-is).
	StdoutFormat string `mapstructure:"stdout_format"`
	// TimeoutSeconds caps one tool run; <= 0 selects the script
	// package's default.
	TimeoutSeconds int `mapstructure:"timeout_seconds"`
	// Env is extra KEY=VALUE pairs appended to the process environment.
	Env []string `mapstructure:"env"`
	// Workdir is the process working directory; empty inherits the
	// platform's.
	Workdir string `mapstructure:"workdir"`
}

// ValidateTools checks the platform config's tools: section the way New
// would, without opening any environment: names must not collide with a
// built-in or with each other, both schema files must be readable, and
// the exec command must be non-empty. Platform entry points call it at
// startup — the boundary for platform config
// (docs/convention/runtime-safety.md §Boundaries) — so an invalid tools:
// entry fails fast with an error naming the offending tool instead of
// surfacing on the first message.
func (f *Factory) ValidateTools() error {
	builtins := make(map[string]struct{}, len(tool.BuiltinToolNames()))
	for _, name := range tool.BuiltinToolNames() {
		builtins[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(f.Tools))
	for _, t := range f.Tools {
		if _, ok := builtins[t.Name]; ok {
			return fmt.Errorf("script tool %q collides with a built-in: %w", t.Name, tool.ErrDuplicateTool)
		}
		if _, ok := seen[t.Name]; ok {
			return fmt.Errorf("script tool %q is declared twice: %w", t.Name, tool.ErrDuplicateTool)
		}
		seen[t.Name] = struct{}{}
		if len(t.Exec.Command) == 0 {
			return fmt.Errorf("script tool %q: exec.command is required", t.Name)
		}
		if _, err := t.spec(); err != nil {
			return fmt.Errorf("script tool %q: %w", t.Name, err)
		}
	}
	return nil
}

// spec builds the script.Spec for one configured script tool, reading
// its JSON Schema documents from disk.
func (t ScriptTool) spec() (script.Spec, error) {
	inputSchema, err := readSchema(t.InputSchema, "input")
	if err != nil {
		return script.Spec{}, err
	}
	outputSchema, err := readSchema(t.OutputSchema, "output")
	if err != nil {
		return script.Spec{}, err
	}
	return script.Spec{
		Name:         t.Name,
		Description:  t.Description,
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Exec: script.ExecSpec{
			Command:      t.Exec.Command,
			StdinFormat:  t.Exec.StdinFormat,
			StdoutFormat: t.Exec.StdoutFormat,
			Timeout:      time.Duration(t.Exec.TimeoutSeconds) * time.Second,
			Env:          t.Exec.Env,
			Workdir:      t.Exec.Workdir,
		},
	}, nil
}

// readSchema reads a JSON Schema document from path. Some script tools
// ship one combined document of the form {"input": ..., "output": ...};
// when the document has that shape, readSchema returns the named half.
// Any other document is returned as-is.
func readSchema(path, half string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw, nil
	}
	if _, ok := doc["input"]; !ok {
		return raw, nil
	}
	if _, ok := doc["output"]; !ok {
		return raw, nil
	}
	if schema, ok := doc[half]; ok {
		return schema, nil
	}
	return raw, nil
}

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
	embedder := f.Embedder
	if embedder == nil {
		embedder = memory.NewHashingEmbedder()
	}
	memSvc := memory.NewService(store, embedder, summarizer)
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
	scriptExec := shell.UnavailableExecutor()
	if f.NewShellExecutor != nil {
		scriptExec = f.NewShellExecutor()
	}
	for _, st := range f.Tools {
		spec, err := st.spec()
		if err != nil {
			return nil, errors.Join(err, store.Close())
		}
		if err := registry.Register(script.New(spec, scriptExec)); err != nil {
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
			_, err := memSvc.Consolidate(ctx)
			return errors.Join(err, memSvc.Close())
		},
	}, nil
}

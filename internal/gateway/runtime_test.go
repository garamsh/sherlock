package gateway_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool"
)

// recordingSink captures outbound messages.
type recordingSink struct {
	mu   sync.Mutex
	msgs []message.Message
}

func (s *recordingSink) Send(_ context.Context, msg message.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
	return nil
}

func (s *recordingSink) messages() []message.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]message.Message(nil), s.msgs...)
}

// replyModel scripts one full turn: a message_send tool call, then a final
// response carrying a writeback block.
func replyModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name:  "message_send",
			Input: json.RawMessage(`{"channel":"test","target":"sender","content":"Message received."}`),
		}}},
		agent.ScriptedStep{Content: "Replied.\n```memory\n" +
			`{"add":[{"content":"handled an inbound message","type":"episodic"}]}` + "\n```"},
	)
}

func TestFactory_CustomSinkReceivesOutboundMessages(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
		Sink:     sink,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)

	require.Len(t, sink.messages(), 1)
	assert.Equal(t, message.Message{
		Channel: "test",
		Target:  "sender",
		Content: "Message received.",
	}, sink.messages()[0])
	// The custom sink replaces the default file outbox entirely.
	_, err = os.Stat(filepath.Join(root, "env-1", gateway.OutboxDir))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFactory_NilSinkWritesOutbox(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(root, "env-1", gateway.OutboxDir))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

// stubEmbedder returns a distinctive short vector so tests can tell it
// apart from the 256-dimensional hashing default.
type stubEmbedder struct{}

// Embed implements memory.Embedder.
func (stubEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func TestFactory_ConfiguredEmbedderReachesMemoryService(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
		Sink:     &recordingSink{},
		Embedder: stubEmbedder{},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)
	require.NoError(t, rt.Close(context.Background()))

	store, err := sqlite.Open(filepath.Join(root, "env-1", gateway.MemoryDBFile))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	active, err := store.Active(context.Background())
	require.NoError(t, err)
	require.Len(t, active, 1, "the scripted turn writes back exactly one memory")
	assert.Equal(t, []float32{1, 0, 0}, active[0].Embedding,
		"the configured embedder, not the hashing default, embedded the memory")
}

func compactionModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{Content: "First reply.\n```memory\n{}\n```"},
		agent.ScriptedStep{Content: "Second reply.\n```memory\n{}\n```"},
	)
}

func TestFactory_HistoryOverflowCompactsIntoSearchableEpisodicMemory(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{
		Root:               root,
		NewModel:           compactionModel,
		Sink:               &recordingSink{},
		MaxHistoryMessages: 4,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)

	first, err := rt.Session.Run(context.Background(), "first overflowing task")
	require.NoError(t, err)
	second, err := rt.Session.Run(context.Background(), "second task continues")
	require.NoError(t, err)
	assert.Equal(t, "First reply.", first)
	assert.Contains(t, second, "Second reply.")
	assert.LessOrEqual(t, len(rt.Session.History()), factory.MaxHistoryMessages+1)

	require.NoError(t, rt.Close(context.Background()))
	store, err := sqlite.Open(filepath.Join(root, "env-1", gateway.MemoryDBFile))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	found, err := svc.Search(context.Background(), "first overflowing task", 10)
	require.NoError(t, err)
	var overflow memory.ScoredMemory
	for _, candidate := range found {
		if candidate.Source == "context-overflow" && strings.Contains(candidate.Content, "first overflowing task") {
			overflow = candidate
			break
		}
	}
	require.NotZero(t, overflow.ID)
	assert.Equal(t, memory.TypeEpisodic, overflow.Type)
	assert.Contains(t, overflow.Content, "first overflowing task")
}

// openAITopLevelForbidden lists the keywords the OpenAI function-calling
// API rejects at the top level of a tool's parameters schema (issue #49).
// The canonical list lives in internal/agent/openai; this alias is kept for
// readability inside the test.
var openAITopLevelForbidden = openai.OpenAITopLevelForbidden

func TestBuiltinTools_DescriptionsAreNonEmpty(t *testing.T) {
	// Nil dependencies suffice: Description is a static document on every
	// built-in tool.
	builtins := []tool.Tool{
		memory.NewTool(nil),
		files.NewTool(nil),
		message.NewSendTool(nil),
		shell.NewTool(nil),
	}
	registry := tool.NewRegistry()
	for _, tl := range builtins {
		require.NoError(t, registry.Register(tl))
	}

	for _, d := range registry.List() {
		assert.NotEmpty(t, d.Description, "tool %q must advertise a description", d.Name)
	}
}

func TestBuiltinTools_InputSchemasSatisfyOpenAISubset(t *testing.T) {
	// Nil dependencies suffice: InputSchema is a static document on every
	// built-in tool.
	builtins := []tool.Tool{
		memory.NewTool(nil),
		files.NewTool(nil),
		message.NewSendTool(nil),
		shell.NewTool(nil),
	}
	for _, tl := range builtins {
		t.Run(tl.Name(), func(t *testing.T) {
			var schema map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(tl.InputSchema(), &schema))
			require.Contains(t, schema, "type")
			assert.JSONEq(t, `"object"`, string(schema["type"]),
				"the API requires top-level type object")
			for _, keyword := range openAITopLevelForbidden {
				assert.NotContains(t, schema, keyword,
					"the API rejects top-level %q", keyword)
			}
		})
	}
}

func TestFactory_BuiltinToolNames(t *testing.T) {
	// The four built-in tools are the public contract surface (ADR 0012).
	// A regression in any one of them is a contract change; this test
	// names them so the contract review gates the change.
	factory := &gateway.Factory{
		Root:     t.TempDir(),
		NewModel: replyModel,
		NewShellExecutor: func() shell.Executor {
			return shell.NewProcessExecutor(shell.DefaultAgentUID, 5*1e9)
		},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	names := make([]string, 0)
	for _, d := range rt.Registry.List() {
		names = append(names, d.Name)
	}
	assert.Equal(t,
		[]string{"files", "memory", "message_send", "shell_exec"},
		names,
		"the four built-in tools join the registry in name-sorted order")
}

// writeScriptToolSchemas writes a minimal input/output JSON Schema pair
// into dir and returns their paths. The input schema satisfies the
// OpenAI function-calling subset (top-level type object, no forbidden
// keywords).
func writeScriptToolSchemas(t *testing.T, dir string) (input, output string) {
	t.Helper()
	input = filepath.Join(dir, "input.json")
	output = filepath.Join(dir, "output.json")
	require.NoError(t, os.WriteFile(input, []byte(
		`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`), 0o644))
	require.NoError(t, os.WriteFile(output, []byte(
		`{"type":"object","properties":{"count":{"type":"integer"}}}`), 0o644))
	return input, output
}

func TestFactory_ScriptToolFromConfig(t *testing.T) {
	root := t.TempDir()
	input, output := writeScriptToolSchemas(t, t.TempDir())
	factory := &gateway.Factory{
		Root:     root,
		NewModel: replyModel,
		Tools: []gateway.ScriptTool{{
			Name:         "wordcount",
			Description:  "Count words in a text.",
			InputSchema:  input,
			OutputSchema: output,
			Exec: gateway.ScriptToolExec{
				Command:        []string{"python3", "tools/wordcount.py"},
				StdinFormat:    "json",
				StdoutFormat:   "json",
				TimeoutSeconds: 30,
			},
		}},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	names := make([]string, 0)
	var scriptToolSchema json.RawMessage
	for _, d := range rt.Registry.List() {
		names = append(names, d.Name)
		if d.Name == "wordcount" {
			scriptToolSchema = d.InputSchema
		}
	}
	assert.Equal(t,
		[]string{"files", "memory", "message_send", "wordcount"},
		names,
		"the script tool joins the built-ins in name-sorted order")

	// The script tool's schema satisfies the same OpenAI subset the
	// built-ins are held to.
	var schema map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(scriptToolSchema, &schema))
	require.Contains(t, schema, "type")
	assert.JSONEq(t, `"object"`, string(schema["type"]),
		"the API requires top-level type object")
	for _, keyword := range openAITopLevelForbidden {
		assert.NotContains(t, schema, keyword,
			"the API rejects top-level %q", keyword)
	}
}

func TestFactory_ScriptToolNameCollisionFailsStartup(t *testing.T) {
	input, output := writeScriptToolSchemas(t, t.TempDir())
	factory := &gateway.Factory{
		Root:     t.TempDir(),
		NewModel: replyModel,
		Tools: []gateway.ScriptTool{{
			Name:         "memory",
			Description:  "Collides with the built-in memory tool.",
			InputSchema:  input,
			OutputSchema: output,
			Exec:         gateway.ScriptToolExec{Command: []string{"true"}},
		}},
	}

	_, err := factory.New(context.Background(), "env-1", nil)
	assert.ErrorIs(t, err, tool.ErrDuplicateTool,
		"a script tool named after a built-in fails startup")
}

func TestFactory_ValidateTools(t *testing.T) {
	input, output := writeScriptToolSchemas(t, t.TempDir())
	valid := gateway.ScriptTool{
		Name:         "wordcount",
		Description:  "Count words in a text.",
		InputSchema:  input,
		OutputSchema: output,
		Exec:         gateway.ScriptToolExec{Command: []string{"true"}},
	}
	colliding := valid
	colliding.Name = "memory"
	missingSchema := valid
	missingSchema.Name = "noschema"
	missingSchema.InputSchema = filepath.Join(t.TempDir(), "missing.json")
	emptyCommand := valid
	emptyCommand.Name = "nocommand"
	emptyCommand.Exec = gateway.ScriptToolExec{}

	tests := []struct {
		name    string
		tools   []gateway.ScriptTool
		wantErr string
		wantDup bool
	}{
		{name: "no tools is valid", tools: nil},
		{name: "valid script tool passes", tools: []gateway.ScriptTool{valid}},
		{
			name:    "collision with a built-in fails",
			tools:   []gateway.ScriptTool{colliding},
			wantErr: `"memory"`,
			wantDup: true,
		},
		{
			name:    "duplicate script-tool names fail",
			tools:   []gateway.ScriptTool{valid, valid},
			wantErr: `"wordcount"`,
			wantDup: true,
		},
		{
			name:    "missing schema file fails",
			tools:   []gateway.ScriptTool{missingSchema},
			wantErr: `"noschema"`,
		},
		{
			name:    "empty exec command fails",
			tools:   []gateway.ScriptTool{emptyCommand},
			wantErr: `"nocommand"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := &gateway.Factory{Root: t.TempDir(), NewModel: replyModel, Tools: tt.tools}
			err := factory.ValidateTools()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr,
				"the error must name the offending tool")
			if tt.wantDup {
				assert.ErrorIs(t, err, tool.ErrDuplicateTool)
			}
		})
	}
}

func TestFactory_ScriptToolSchemaDocument(t *testing.T) {
	dir := t.TempDir()
	combined := filepath.Join(dir, "combined.json")
	require.NoError(t, os.WriteFile(combined, []byte(
		`{"input":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]},`+
			`"output":{"type":"object","properties":{"title":{"type":"string"}}}}`), 0o644))
	plain := filepath.Join(dir, "plain.json")
	require.NoError(t, os.WriteFile(plain, []byte(
		`{"type":"object","properties":{"text":{"type":"string"}}}`), 0o644))

	tests := []struct {
		name       string
		schemaPath string
		wantInput  string
		wantOutput string
	}{
		{
			name:       "combined document returns the named half",
			schemaPath: combined,
			wantInput:  `{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`,
			wantOutput: `{"type":"object","properties":{"title":{"type":"string"}}}`,
		},
		{
			name:       "plain document passes through as-is",
			schemaPath: plain,
			wantInput:  `{"type":"object","properties":{"text":{"type":"string"}}}`,
			wantOutput: `{"type":"object","properties":{"text":{"type":"string"}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := &gateway.Factory{
				Root:     t.TempDir(),
				NewModel: replyModel,
				Tools: []gateway.ScriptTool{{
					Name:         "fetch",
					Description:  "Fetch a page.",
					InputSchema:  tt.schemaPath,
					OutputSchema: tt.schemaPath,
					Exec:         gateway.ScriptToolExec{Command: []string{"true"}},
				}},
			}
			rt, err := factory.New(context.Background(), "env-1", nil)
			require.NoError(t, err)
			defer func() {
				assert.NoError(t, rt.Close(context.Background()))
			}()

			var inputSchema, outputSchema json.RawMessage
			for _, d := range rt.Registry.List() {
				if d.Name == "fetch" {
					inputSchema = d.InputSchema
					outputSchema = d.OutputSchema
				}
			}
			assert.JSONEq(t, tt.wantInput, string(inputSchema))
			assert.JSONEq(t, tt.wantOutput, string(outputSchema))
		})
	}
}

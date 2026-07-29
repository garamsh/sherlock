package gateway_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/sandbox"
	"github.com/garamsh/gagent/internal/tool"
)

// stubSandbox substitutes the Docker daemon; the scripted model below
// never calls sandbox_exec, so containers are never created.
type stubSandbox struct{}

func (stubSandbox) Create(context.Context, sandbox.Config) (string, error) { return "stub", nil }
func (stubSandbox) Exec(context.Context, string, []string, time.Duration) (sandbox.Result, error) {
	return sandbox.Result{ExitCode: 0}, nil
}
func (stubSandbox) Destroy(context.Context, string) error { return nil }

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
		Sandbox:  stubSandbox{},
		Image:    "stub",
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
		Sandbox:  stubSandbox{},
		Image:    "stub",
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
		Sandbox:            stubSandbox{},
		Image:              "stub",
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

// stubTool is a static extra tool for registry wiring tests.
type stubTool struct {
	name   string
	called atomic.Bool
}

func (t *stubTool) Name() string                  { return t.name }
func (t *stubTool) Description() string           { return "" }
func (t *stubTool) InputSchema() json.RawMessage  { return nil }
func (t *stubTool) OutputSchema() json.RawMessage { return nil }
func (t *stubTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	t.called.Store(true)
	return json.RawMessage(`{"ok":true}`), nil
}

// extraToolModel scripts one turn calling the extra tool, then a final
// response.
func extraToolModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
			Name:  "extra_tool",
			Input: json.RawMessage(`{}`),
		}}},
		agent.ScriptedStep{Content: "Done."},
	)
}

func TestFactory_ExtraToolsJoinTheRegistry(t *testing.T) {
	extra := &stubTool{name: "extra_tool"}
	factory := &gateway.Factory{
		Root:     t.TempDir(),
		NewModel: extraToolModel,
		Sandbox:  stubSandbox{},
		Image:    "stub",
		Tools:    []tool.Tool{extra},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)
	assert.True(t, extra.called.Load())
}

func TestFactory_ExtraToolNameCollisionFails(t *testing.T) {
	factory := &gateway.Factory{
		Root:     t.TempDir(),
		NewModel: extraToolModel,
		Sandbox:  stubSandbox{},
		Image:    "stub",
		Tools:    []tool.Tool{&stubTool{name: files.ToolName}},
	}
	_, err := factory.New(context.Background(), "env-1", nil)
	require.ErrorIs(t, err, tool.ErrDuplicateTool)
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
		sandbox.NewExecTool(nil, sandbox.Config{}, 0),
		message.NewSendTool(nil),
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
		sandbox.NewExecTool(nil, sandbox.Config{}, 0),
		message.NewSendTool(nil),
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

// schemaStubTool is a static tool carrying an arbitrary InputSchema — the
// kind of descriptor an MCP server might hand back at discovery time.
type schemaStubTool struct {
	name        string
	description string
	schema      json.RawMessage
}

func (t *schemaStubTool) Name() string                  { return t.name }
func (t *schemaStubTool) Description() string           { return t.description }
func (t *schemaStubTool) InputSchema() json.RawMessage  { return t.schema }
func (t *schemaStubTool) OutputSchema() json.RawMessage { return nil }
func (t *schemaStubTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"ok":true}`), nil
}

// TestFactory_MCPStyleToolIsSanitizedForOpenAI is the MCP-discovered
// companion to TestBuiltinTools_InputSchemasSatisfyOpenAISubset: a tool
// registered through Factory.Tools whose schema uses a top-level keyword
// the OpenAI function-calling API rejects must reach the wire in the
// sanitized form, not the original. Issue #55.
func TestFactory_MCPStyleToolIsSanitizedForOpenAI(t *testing.T) {
	var captured struct {
		sync.Mutex
		body []byte
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			captured.Lock()
			captured.body = body
			captured.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer fake.Close()

	model, err := openai.New(openai.Config{
		BaseURL: fake.URL + "/v1",
		Model:   "test-model",
		APIKey:  "sk-test",
	})
	require.NoError(t, err)

	// Top-level oneOf with no top-level type: the API rejects both.
	mcpSchema := json.RawMessage(`{
		"oneOf": [
			{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},
			{"type":"object","properties":{"b":{"type":"integer"}},"required":["b"]}
		]
	}`)
	extra := &schemaStubTool{
		name:        "mcp_tool",
		description: "an mcp tool with a top-level oneOf",
		schema:      mcpSchema,
	}

	factory := &gateway.Factory{
		Root:     t.TempDir(),
		NewModel: func(string) agent.Model { return model },
		Sandbox:  stubSandbox{},
		Image:    "stub",
		Tools:    []tool.Tool{extra},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() { assert.NoError(t, rt.Close(context.Background())) }()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err, "the session must not fail on an MCP-style schema")

	captured.Lock()
	body := captured.body
	captured.Unlock()
	require.NotEmpty(t, body, "the openai provider must have been called")

	var req struct {
		Tools []struct {
			Function struct {
				Name       string          `json:"name"`
				Parameters json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(body, &req))
	require.NotEmpty(t, req.Tools)
	var params map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(req.Tools[0].Function.Parameters, &params))
	for _, keyword := range openAITopLevelForbidden {
		assert.NotContains(t, params, keyword,
			"the provider must drop top-level %q before the API sees it", keyword)
	}
	require.Contains(t, params, "type")
	assert.JSONEq(t, `"object"`, string(params["type"]),
		"the sanitized parameters must declare top-level type object")
}

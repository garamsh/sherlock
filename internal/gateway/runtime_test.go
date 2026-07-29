package gateway_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

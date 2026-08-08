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
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
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

// memoryPath returns the memory file path inside root the test factories
// use; the outbox lands in root/outbox beside it.
func memoryPath(root string) string { return filepath.Join(root, "memory.db") }

func TestFactory_CustomSinkReceivesOutboundMessages(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,
		Sink:       sink,
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
	_, err = os.Stat(filepath.Join(root, gateway.OutboxDir))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestFactory_NilSinkWritesOutbox(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)

	entries, err := os.ReadDir(filepath.Join(root, gateway.OutboxDir))
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestFactory_NewSinkBuildsAgentSink(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	var gotAgentID string
	var gotFallback *gateway.Outbox
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,
		NewSink: func(agentID string, fallback *gateway.Outbox) message.Sink {
			gotAgentID = agentID
			gotFallback = fallback
			return sink
		},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)

	require.Len(t, sink.messages(), 1)
	assert.Equal(t, "env-1", gotAgentID)
	assert.Equal(t, filepath.Join(root, gateway.OutboxDir), gotFallback.Dir(),
		"the agent sink receives the default file outbox as its fallback")
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
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,
		Sink:       &recordingSink{},
		Embedder:   stubEmbedder{},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)
	require.NoError(t, rt.Close(context.Background()))

	store, err := sqlite.Open(memoryPath(root))
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

// consolidateModel scripts one turn whose writeback adds two episodic
// memories, reaching the consolidation cluster threshold.
func consolidateModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{Content: "Done.\n```memory\n" +
			`{"add":[{"content":"deployed v1 to staging","type":"episodic"},` +
			`{"content":"staging deploy failed on migration","type":"episodic"}]}` + "\n```"},
	)
}

// TestRuntime_CloseConsolidatesEpisodicCluster pins the ADR 0015 shutdown
// trigger: closing the runtime compresses the active episodic cluster
// into one semantic parent and archives the originals.
func TestRuntime_CloseConsolidatesEpisodicCluster(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   consolidateModel,
		Sink:       &recordingSink{},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)

	_, err = rt.Session.Run(context.Background(), "deploy work")
	require.NoError(t, err)
	require.NoError(t, rt.Close(context.Background()))

	store, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	active, err := store.Active(context.Background())
	require.NoError(t, err)
	require.Len(t, active, 1,
		"the two episodic originals are archived; only the semantic parent stays active")
	assert.Equal(t, memory.TypeSemantic, active[0].Type)
	assert.Equal(t, memory.CompressSource, active[0].Source)
	assert.Contains(t, active[0].Content, "deployed v1 to staging")
	assert.Contains(t, active[0].Content, "staging deploy failed on migration")
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
		MemoryPath:         memoryPath(root),
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
	store, err := sqlite.Open(memoryPath(root))
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

// stubTool is a minimal discovered-tool stand-in: a static descriptor
// whose Call never runs in these tests.
type stubTool struct{ name string }

func (t stubTool) Name() string                 { return t.name }
func (t stubTool) Description() string          { return "a discovered tool" }
func (t stubTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t stubTool) OutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t stubTool) Call(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// TestFactory_RegistryHoldsMouthPlusDiscoveredTools pins ADR 0018: the
// agent's registry carries no built-in computer tools — message_send (the
// mouth, in-process) plus every discovered remote tool.
func TestFactory_RegistryHoldsMouthPlusDiscoveredTools(t *testing.T) {
	factory := &gateway.Factory{
		MemoryPath: memoryPath(t.TempDir()),
		NewModel:   replyModel,
		Tools:      []tool.Tool{stubTool{name: "shell_exec"}, stubTool{name: "files"}},
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
	assert.Equal(t, []string{"files", "message_send", "shell_exec"}, names,
		"the mouth plus the discovered tools join the registry in name-sorted order")
}

// TestFactory_DiscoveredToolCollidingWithMouthFailsStartup pins the
// registry boundary: a tool server advertising message_send collides
// with the in-process mouth and fails startup instead of shadowing it.
func TestFactory_DiscoveredToolCollidingWithMouthFailsStartup(t *testing.T) {
	factory := &gateway.Factory{
		MemoryPath: memoryPath(t.TempDir()),
		NewModel:   replyModel,
		Tools:      []tool.Tool{stubTool{name: "message_send"}},
	}
	_, err := factory.New(context.Background(), "env-1", nil)
	assert.ErrorIs(t, err, tool.ErrDuplicateTool)
}

func TestMessageSendTool_DescriptionAndSchemaSatisfyOpenAISubset(t *testing.T) {
	tl := message.NewSendTool(nil)
	assert.NotEmpty(t, tl.Description(), "the mouth must advertise a description")

	var schema map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(tl.InputSchema(), &schema))
	require.Contains(t, schema, "type")
	assert.JSONEq(t, `"object"`, string(schema["type"]),
		"the API requires top-level type object")
	for _, keyword := range openai.OpenAITopLevelForbidden {
		assert.NotContains(t, schema, keyword,
			"the API rejects top-level %q", keyword)
	}
}

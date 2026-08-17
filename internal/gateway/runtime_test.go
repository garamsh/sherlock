package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/agent/openai"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool"
	"github.com/garamsh/gagent/internal/tool/tooltest"
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
		agent.ScriptedStep{Content: `{"reply":"Replied.","memory_ops":[{"address":{` +
			`"subject":"the test channel","question":"what did the test channel ask for",` +
			`"restriction":null},"value":"an acknowledgement","operation":null}]}`},
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
		Tools:      mouthClientSink(sink),
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
	outbox := &fileSink{dir: gateway.DirFor(memoryPath(root))}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,
		Tools:      mouthClientSink(outbox),
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

// TestFactory_OutboxConstruction pins the migration: the runtime no
// longer takes a sink. The platform tool client absorbs the message
// delivery path; tests that want to capture outbound messages wire
// the message_send tool directly to their sink.
func TestFactory_OutboxConstruction(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,
		Tools:      mouthClientSink(sink),
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
}

// TestQueue_ThresholdConsolidationRunsAfterTurn is the ADR 0011
// acceptance case with a real store: seeded past ConsolidationThreshold,
// one completed turn consolidates the episodic cluster — no shutdown.
func TestQueue_ThresholdConsolidationRunsAfterTurn(t *testing.T) {
	root := t.TempDir()

	// Seed the store past the threshold, then close it; the runtime
	// opens the same file. Each note carries unique tokens so inserts
	// stay below the supersession similarity (ADR 0006).
	seed, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	seedSvc := memory.NewService(seed, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	for i := range memory.ConsolidationThreshold {
		_, err := seedSvc.Store(context.Background(), memory.StoreInput{
			Content:         fmt.Sprintf("episodic note %d w%d x%d y%d z%d q%d", i, i, i, i, i, i),
			Type:            memory.TypeEpisodic,
			ImportanceScore: 0.5,
			Source:          "test",
		})
		require.NoError(t, err)
	}
	require.NoError(t, seedSvc.Close())

	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,

		Tools: mouthClientSink(sink),
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)

	_, err = rt.Session.Run(context.Background(), "one turn")
	require.NoError(t, err)
	require.NoError(t, rt.ConsolidateIfDue(context.Background()),
		"the after-turn trigger runs against the real store")

	store, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	active, err := store.Active(context.Background())
	require.NoError(t, err)
	var episodic, semantic int
	for _, m := range active {
		switch m.Type {
		case memory.TypeEpisodic:
			episodic++
		case memory.TypeSemantic:
			semantic++
		}
	}
	assert.Zero(t, episodic, "the whole episodic cluster archived")
	assert.Positive(t, semantic, "the cluster compressed into semantic parents")

	require.NoError(t, rt.Close(context.Background()))
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
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   replyModel,

		Embedder: stubEmbedder{},
		Tools:    mouthClientSink(sink),
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

// consolidateModel scripts one turn that states no fact: the episodic
// cluster the consolidation trigger works over is seeded into the store
// directly, because a writeback states facts at addresses and never
// episodes (ADR 0018).
func consolidateModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{Content: `{"reply":"Done.","memory_ops":[]}`},
	)
}

// seedEpisodicCluster writes the two episodic memories the consolidation
// threshold works over into the store the factory will open.
func seedEpisodicCluster(t *testing.T, path string, contents ...string) {
	t.Helper()
	store, err := sqlite.Open(path)
	require.NoError(t, err)
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	for _, content := range contents {
		_, err := svc.Store(context.Background(), memory.StoreInput{
			Content:         content,
			Type:            memory.TypeEpisodic,
			ImportanceScore: memory.SalienceAddEpisodic,
			Source:          "agent",
		})
		require.NoError(t, err)
	}
	require.NoError(t, svc.Close())
}

// TestRuntime_CloseConsolidatesEpisodicCluster pins the ADR 0011 shutdown
// trigger: closing the runtime compresses the active episodic cluster
// into one semantic parent and archives the originals.
func TestRuntime_CloseConsolidatesEpisodicCluster(t *testing.T) {
	root := t.TempDir()
	seedEpisodicCluster(t, memoryPath(root),
		"deployed v1 to staging", "staging deploy failed on migration")
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   consolidateModel,

		Tools: mouthClientSink(sink),
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
		agent.ScriptedStep{Content: `{"reply":"First reply.","memory_ops":[]}`},
		agent.ScriptedStep{Content: `{"reply":"Second reply.","memory_ops":[]}`},
	)
}

// generatedSummarizer stands in for a model-backed summarizer: it
// reports Generated true, which is what licenses the context-overflow
// write (ADR 0006, ADR 0004).
type generatedSummarizer struct{ text string }

func (g generatedSummarizer) Summarize(_ context.Context, _ []string) (memory.Summary, error) {
	return memory.Summary{Text: g.text, Generated: true}, nil
}

// TestFactory_HistoryOverflowStoresNothingWithoutARealSummarizer pins
// the shipped default: no summarizer is configured, so compaction holds
// a join of the evicted turns rather than a summary, and stores none of
// it. Until #541 this test asserted the opposite — that the evicted
// user text was searchable — which is how the defect survived.
func TestFactory_HistoryOverflowStoresNothingWithoutARealSummarizer(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   compactionModel,

		Tools:              mouthClientSink(sink),
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
	assert.LessOrEqual(t, len(rt.Session.History()), factory.MaxHistoryMessages+1,
		"the turns still leave the window; only the write is refused")

	require.NoError(t, rt.Close(context.Background()))
	store, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	found, err := svc.Search(context.Background(), "first overflowing task", 10)
	require.NoError(t, err)
	for _, candidate := range found {
		assert.NotEqual(t, "context-overflow", candidate.Source,
			"an inbound message the model never stated as a fact stays out of the store (ADR 0006)")
	}
}

// TestFactory_HistoryOverflowStoresAModelSummary pins the other half at
// the wiring level: with a summarizer that reports a generated summary,
// the episodic memory ADR 0006 describes is written and searchable.
func TestFactory_HistoryOverflowStoresAModelSummary(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   compactionModel,

		Tools:              mouthClientSink(sink),
		MaxHistoryMessages: 4,
		Summarizer:         generatedSummarizer{text: "the session opened with an overflowing task"},
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)

	_, err = rt.Session.Run(context.Background(), "first overflowing task")
	require.NoError(t, err)
	_, err = rt.Session.Run(context.Background(), "second task continues")
	require.NoError(t, err)

	require.NoError(t, rt.Close(context.Background()))
	store, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	svc := memory.NewService(store, memory.NewHashingEmbedder(), memory.NewConcatSummarizer())
	found, err := svc.Search(context.Background(), "overflowing task", 10)
	require.NoError(t, err)
	var overflow memory.ScoredMemory
	for _, candidate := range found {
		if candidate.Source == "context-overflow" {
			overflow = candidate
			break
		}
	}
	require.NotZero(t, overflow.ID)
	assert.Equal(t, memory.TypeEpisodic, overflow.Type)
	assert.Equal(t, "the session opened with an overflowing task", overflow.Content,
		"what is stored is the summary, not the turns it was made from")
}

// stubClient is a minimal platform-tool-client stand-in for tests
// that don't exercise the protocol: a static descriptor list and a
// single call handler. The shape matches the agent.ToolClient interface
// the runtime accepts.
type stubClient struct {
	descriptors []tool.Descriptor
	err         error
}

func (s stubClient) List() []tool.Descriptor {
	out := make([]tool.Descriptor, len(s.descriptors))
	copy(out, s.descriptors)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s stubClient) Call(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), s.err
}

// outboundSink is what a mouthClient delivers through. It is a test
// seam and has no production counterpart: outbound delivery is the
// message_send tool writing a JSON file into the outbox directory, so
// nothing in internal/ implements this.
type outboundSink interface {
	Send(ctx context.Context, msg message.Message) error
}

// fileSink is the stand-in for that tool: it writes one JSON file per
// message into dir, named so a reader sees delivery order.
type fileSink struct {
	dir string
	mu  sync.Mutex
	seq int
}

func (f *fileSink) Send(_ context.Context, msg message.Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.seq++
	name := fmt.Sprintf("%d-%06d.json", time.Now().UnixNano(), f.seq)
	f.mu.Unlock()
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(f.dir, name), payload, 0o644)
}

// mouthClient is a stubClient that forwards message_send calls to a
// sink. It mirrors the message_send tool's contract (in/out
// schemas) so the test factories exercise the structural shape —
// descriptor fetched over the wire — without spawning the binary.
type mouthClient struct {
	descriptors []tool.Descriptor
	sink        outboundSink
}

func (m mouthClient) List() []tool.Descriptor {
	out := append([]tool.Descriptor(nil), m.descriptors...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m mouthClient) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	if name != "message_send" {
		return nil, errors.New("mouthClient: unknown tool " + name)
	}
	var msg message.Message
	if err := json.Unmarshal(input, &msg); err != nil {
		return nil, err
	}
	if err := m.sink.Send(context.Background(), msg); err != nil {
		return nil, err
	}
	return json.RawMessage(`{"status":"queued"}`), nil
}

// mouthClientSink wires a sink into a mouthClient so the test factories
// can stand in for the message_send delivery endpoint without spawning
// the binary.
func mouthClientSink(sink outboundSink) agent.ToolClient {
	return mouthClient{
		descriptors: []tool.Descriptor{
			{Name: "message_send", Description: "Queue an outbound message for delivery to a channel and target."},
		},
		sink: sink,
	}
}

// TestFactory_ToolClientDescriptorsReachSession pins the structural
// move (ADR 0017 §Decision): the agent's runtime surfaces the
// descriptors the platform tool client fetched, never an in-process
// map the agent compiled itself.
func TestFactory_ToolClientDescriptorsReachSession(t *testing.T) {
	client := stubClient{descriptors: []tool.Descriptor{
		{Name: "playwright", Description: "browser automation"},
		{Name: "files", Description: "read/write files"},
	}}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(t.TempDir()),
		NewModel:   replyModel,
		Tools:      client,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	names := make([]string, 0)
	for _, d := range rt.Tools.List() {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"files", "playwright"}, names,
		"the tool client surfaces only what the operator installed; the agent carries no compile-time list")
}

// TestFactory_NilToolClientIsFatal pins the failure mode: an agent
// runtime with no tool client configured calls into the no-tool stub
// and every tool call fails closed.
func TestFactory_NilToolClientIsFatal(t *testing.T) {
	factory := &gateway.Factory{
		MemoryPath: memoryPath(t.TempDir()),
		NewModel:   replyModel,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Tools.Call(context.Background(), "any", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tool client configured")
}

// TestMessageSendTool_DescriptionAndSchemaSatisfyOpenAISubset reads the
// manifest the registry loads and shows the model, so a forbidden
// keyword added to the shipped contract fails here.
func TestMessageSendTool_DescriptionAndSchemaSatisfyOpenAISubset(t *testing.T) {
	input, _ := tooltest.ManifestSchemas(t, tooltest.ReadManifest(t, "message_send"))
	var schema map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(input, &schema))
	require.Contains(t, schema, "type")
	assert.JSONEq(t, `"object"`, string(schema["type"]),
		"the API requires top-level type object")
	for _, keyword := range openai.OpenAITopLevelForbidden {
		assert.NotContains(t, schema, keyword,
			"the API rejects top-level %q", keyword)
	}
}

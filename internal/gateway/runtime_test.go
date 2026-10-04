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

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/agent/openai"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/memory"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/message"
	"github.com/garamsh/sherlock/internal/tool"
	"github.com/garamsh/sherlock/internal/tool/tooltest"
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
		writebackStep(`{"reply":"Replied.","note":null}`),
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
	// Delivery belongs to the tool client, so a tool client routing
	// message_send into memory means nothing writes files: the factory
	// creates no outbox of its own.
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

// TestQueue_CloseClosesTheStore pins the served path: SIGTERM cancels
// the context, the queue closes, and the runtime's Close has to leave a
// checkpointed database behind. The store is a mounted volume
// (`deployment.md`), so an unclosed handle hands the next agent a
// `memory.db` whose last writes are still in a `-wal` beside it (#568).
func TestQueue_CloseClosesTheStore(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   func(string) agent.Model { return agent.NewScriptedModel() },
		Tools:      noTools(),
	}
	q, err := gateway.NewQueue(factory, "env-1", newTestLogger())
	require.NoError(t, err)

	require.NoError(t, q.Close())

	assertStoreCheckpointed(t, memoryPath(root))
}

func compactionModel(_ string) agent.Model {
	return summaryScript(
		writebackStep(`{"reply":"First reply.","note":null}`),
		writebackStep(`{"reply":"Second reply.","note":null}`),
	)
}

// TestFactory_HistoryOverflowStoresNothing pins ADR 0006's curation
// rule where it used to be most at risk: the turns that leave the
// context window enter no store. It held by a provenance check until
// #582 and holds by construction now — compaction takes no memory
// service, so there is no path for an evicted turn to reach the store.
// Until #541 this test asserted the opposite, that the evicted user text
// was searchable, which is how the defect survived.
func TestFactory_HistoryOverflowStoresNothing(t *testing.T) {
	root := t.TempDir()
	sink := &recordingSink{}
	factory := &gateway.Factory{
		MemoryPath: memoryPath(root),
		NewModel:   compactionModel,
		Embedder:   wordEmbedder{},

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
	assert.LessOrEqual(t, len(rt.Session.History()), factory.MaxHistoryMessages+2,
		"the turns still leave the window; only the write is refused — "+
			"the bound is the cap, the instructions that survive the cut (#642), "+
			"and the in-flight assistant reply")

	require.NoError(t, rt.Close(context.Background()))
	store, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, store.Close())
	}()
	active, err := store.Active(context.Background())
	require.NoError(t, err)
	for _, m := range active {
		assert.NotEqual(t, "context-overflow", m.Source,
			"an inbound message the model never stated as a fact stays out of the store (ADR 0006)")
	}
}

// A completed turn is available as source-backed history after restart, while
// addressed rows are not a second active memory authority.
func TestFactory_ConversationSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	factory := &gateway.Factory{MemoryPath: memoryPath(root), NewModel: func(string) agent.Model {
		return agent.NewScriptedModel(writebackStep(`{"reply":"The observatory closes at dusk.","note":null}`))
	}, Tools: noTools(), MaxHistoryMessages: 2}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	_, err = rt.Session.Run(context.Background(), "When does the observatory close?")
	require.NoError(t, err)
	require.NoError(t, rt.Close(context.Background()))
	store, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	sources, err := store.Recent(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.Equal(t, agent.RoleUser, sources[0].Role)
	assert.Equal(t, "The observatory closes at dusk.", sources[1].Content)
	pending, err := store.PendingSources(context.Background())
	require.NoError(t, err)
	require.Len(t, pending, 2)
	assert.Contains(t, pending[0].Content, "observatory")
	require.NoError(t, store.Close())
	restarted, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer restarted.Close(context.Background())
	assert.Contains(t, restarted.Session.History(), agent.Message{Role: agent.RoleUser, Content: "When does the observatory close?"})
}

func TestFactory_DisabledCompactionRestoresAllHistory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "disabled", limit: -1, want: []string{"First question?", "Second question?"}},
		{name: "bounded", limit: 2, want: []string{"Second question?"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			factory := &gateway.Factory{MemoryPath: memoryPath(root), NewModel: func(string) agent.Model {
				return summaryScript(writebackStep(`{"reply":"Answer.","note":null}`))
			}, Embedder: wordEmbedder{}, Tools: noTools(), MaxHistoryMessages: tc.limit}
			for _, question := range []string{"First question?", "Second question?"} {
				runtime, err := factory.New(context.Background(), "env-1", nil)
				require.NoError(t, err)
				_, err = runtime.Session.Run(context.Background(), question)
				require.NoError(t, err)
				require.NoError(t, runtime.Close(context.Background()))
			}
			restarted, err := factory.New(context.Background(), "env-1", nil)
			require.NoError(t, err)
			defer restarted.Close(context.Background())
			var got []string
			for _, message := range restarted.Session.History() {
				if message.Role == agent.RoleUser {
					got = append(got, message.Content)
				}
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFactory_RefusesAddressedStoreWithoutChangingIt(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	seed, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	written := time.Now()
	_, _, err = seed.Write(ctx, memory.Memory{Address: memory.Address{Subject: "user", Question: "what is the favorite telescope"}, Value: "Kepler", ImportanceScore: memory.SalienceJoin, Timestamp: written, Source: "old"}, nil, written)
	require.NoError(t, err)
	require.NoError(t, seed.Close())
	factory := &gateway.Factory{MemoryPath: memoryPath(root), NewModel: func(string) agent.Model { return agent.NewScriptedModel() }, Tools: noTools()}
	_, err = factory.New(ctx, "env-1", nil)
	require.ErrorIs(t, err, sqlite.ErrAddressedStore)
	reopened, err := sqlite.Open(memoryPath(root))
	require.NoError(t, err)
	defer reopened.Close()
	held, err := reopened.Active(ctx)
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "Kepler", held[0].Value)
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

// noTools is the empty tool surface: nothing installed, and any call
// refused. A runtime built on it exercises the reasoning loop and the
// memory hooks with no tool registry behind them.
func noTools() agent.ToolClient {
	return stubClient{err: errors.New("no tool installed")}
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
// schemas) so the test factories exercise the structural shape — the
// descriptor the registry reads from a manifest — without spawning the
// binary.
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

// toolCapturingModel records the descriptors each model call was shown
// and runs the scripted turn the rest of this file's factories run.
type toolCapturingModel struct {
	inner agent.Model
	seen  [][]tool.Descriptor
}

func newToolCapturingModel() *toolCapturingModel {
	return &toolCapturingModel{inner: agent.NewScriptedModel(
		writebackStep(`{"reply":"Replied.","note":null}`),
	)}
}

func (m *toolCapturingModel) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.seen = append(m.seen, append([]tool.Descriptor(nil), tools...))
	return m.inner.Chat(ctx, messages, tools)
}

// actTools returns the descriptors the act loop was shown: the turn's
// last model call.
func (m *toolCapturingModel) actTools() []tool.Descriptor {
	return m.seen[len(m.seen)-1]
}

// TestFactory_ToolClientDescriptorsReachSession pins the structural
// move (ADR 0017 §Decision): the session the factory builds drives the
// tool client the factory was handed, so the model is shown the
// descriptors that client fetched and never an in-process map the agent
// compiled itself. The model is where this is read, because the model
// is the far end of the wiring under test; reading it back off the
// runtime asserts only that the factory copied a field.
func TestFactory_ToolClientDescriptorsReachSession(t *testing.T) {
	client := stubClient{descriptors: []tool.Descriptor{
		{Name: "playwright", Description: "browser automation"},
		{Name: "files", Description: "read/write files"},
	}}
	model := newToolCapturingModel()
	factory := &gateway.Factory{
		MemoryPath: memoryPath(t.TempDir()),
		NewModel:   func(string) agent.Model { return model },
		Tools:      client,
	}
	rt, err := factory.New(context.Background(), "env-1", nil)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rt.Close(context.Background()))
	}()

	_, err = rt.Session.Run(context.Background(), "hi")
	require.NoError(t, err)

	require.NotEmpty(t, model.seen, "the turn reaches the model")
	names := make([]string, 0)
	for _, d := range model.actTools() {
		// The reserved calls are the platform's addition to what the
		// model sees, not part of the operator's list.
		if d.Name == agent.MemoryWriteTool || d.Name == agent.MemoryInspectTool || d.Name == agent.MemorySearchTool {
			continue
		}
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"files", "playwright"}, names,
		"the tool client surfaces only what the operator installed; the agent carries no compile-time list")
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

// writebackStep scripts the call a turn ends with: the reserved
// writeback tool carrying the response document (issue #615).
func writebackStep(document string) agent.ScriptedStep {
	return agent.ScriptedStep{ToolCalls: []agent.ToolCall{{
		Name:  agent.MemoryWriteTool,
		Input: json.RawMessage(document),
	}}}
}

// TestFactory_RequireStoreRefusesAnAbsentStore holds the opt-in refusal at
// the composition root (ADR 0025): with RequireStore set, a store that is
// absent — its directory included — is refused before any model is built
// or catch-up runs, and nothing is created. The control is the same
// factory without RequireStore, which creates the directory and the store
// as every first start does.
func TestFactory_RequireStoreRefusesAnAbsentStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	models := 0
	factory := &gateway.Factory{
		MemoryPath:   memoryPath(dir),
		RequireStore: true,
		NewModel: func(string) agent.Model {
			models++
			return agent.NewScriptedModel()
		},
		Tools: noTools(),
	}
	_, err := factory.New(context.Background(), "a", nil)
	require.ErrorIs(t, err, sqlite.ErrStoreUninitialized)
	assert.Zero(t, models, "a refused start built the session model")
	assert.NoDirExists(t, dir)

	factory.RequireStore = false
	rt, err := factory.New(context.Background(), "a", nil)
	require.NoError(t, err, "control: without RequireStore the first start creates the store")
	require.NoError(t, rt.Close(context.Background()))
	assert.FileExists(t, memoryPath(dir))
}

// TestFactory_RequireStoreStartsOnAnInitializedStore holds that the
// refusal leaves an initialized store alone: a factory with RequireStore
// set starts on a store an earlier start created, and reads the message
// that start committed.
func TestFactory_RequireStoreStartsOnAnInitializedStore(t *testing.T) {
	path := memoryPath(t.TempDir())
	store, err := sqlite.OpenConversation(path, nil)
	require.NoError(t, err)
	require.NoError(t, store.RecordTurn(context.Background(),
		[]agent.SourceMessage{{Role: agent.RoleUser, ExternalID: "m1", Sender: "s", Content: "remember cedar"}}, nil))
	require.NoError(t, store.Close())

	factory := &gateway.Factory{
		MemoryPath:   path,
		RequireStore: true,
		NewModel:     func(string) agent.Model { return agent.NewScriptedModel() },
		Tools:        noTools(),
	}
	rt, err := factory.New(context.Background(), "a", nil)
	require.NoError(t, err)
	defer func() { assert.NoError(t, rt.Close(context.Background())) }()
	held, same, err := rt.Committed(context.Background(), "s", "m1", "remember cedar")
	require.NoError(t, err)
	assert.True(t, held && same, "the start opened a store other than the initialized one")
}

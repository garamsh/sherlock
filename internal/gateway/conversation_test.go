package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/gateway"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

type evidenceModel struct {
	inner agent.Model
	calls int
}

func (m *evidenceModel) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.calls++
	if m.calls == 2 {
		found := false
		for _, message := range messages {
			if message.Role == agent.RoleTool && message.Name == agent.MemoryInspectTool && strings.Contains(message.Content, "Correction: my preferred telescope is the Vega-2 reflector.") {
				found = true
			}
		}
		if !found {
			return agent.Response{}, errors.New("exact source was not inspected before answer")
		}
	}
	return m.inner.Chat(ctx, messages, tools)
}

type conversationEvents struct{ events []agent.Event }

func (s *conversationEvents) Emit(_ context.Context, e agent.Event) { s.events = append(s.events, e) }

func TestConversation_ParaphraseCorrectionAndExactSource(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	first := summaryScript(
		writebackStep(`{"reply":"Noted.","note":null}`),
		writebackStep(`{"reply":"Corrected.","note":null}`),
	)
	f := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return first }, Embedder: wordEmbedder{}, Tools: noTools(), MaxHistoryMessages: 2}
	rt, err := f.New(ctx, "a", nil)
	require.NoError(t, err)
	_, err = rt.Session.Run(agent.WithInboundSources(ctx, checkpointSources("My preferred telescope is the Kepler-9 refractor.", 3)), "Remember the telescope preference.")
	require.NoError(t, err)
	_, err = rt.Session.Run(agent.WithInboundSources(ctx, checkpointSources("Correction: my preferred telescope is the Vega-2 reflector.", 3)), "Remember the correction.")
	require.NoError(t, err)
	require.LessOrEqual(t, len(rt.Session.History()), 4)
	require.NoError(t, rt.Close(ctx))
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	hits, err := store.Search(ctx, "preferred telescope", 5)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(hits), 2)
	require.LessOrEqual(t, len(hits), 5)
	var correction agent.ConversationHit
	for _, hit := range hits {
		if strings.Contains(hit.Summary, "Vega-2") {
			correction = hit
			break
		}
	}
	require.NotZero(t, correction.ID, "the correction is offered among the candidates")
	source, err := store.Source(ctx, correction.SourceFirst)
	require.NoError(t, err)
	assert.Equal(t, "Correction: my preferred telescope is the Vega-2 reflector.", source.Content)
	require.NoError(t, store.Close())

	model := summaryScript(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemoryInspectTool, Input: json.RawMessage(`{"id":5}`)}}},
		writebackStep(`{"reply":"You corrected it to the Vega-2 reflector; your exact words were: 'Correction: my preferred telescope is the Vega-2 reflector.'","note":null}`),
	)
	events := &conversationEvents{}
	f.NewModel = func(string) agent.Model { return &evidenceModel{inner: model} }
	restarted, err := f.New(ctx, "a", events)
	require.NoError(t, err)
	reply, err := restarted.Session.Run(ctx, "What did I say exactly about the optical instrument I prefer?")
	require.NoError(t, err)
	assert.Contains(t, reply, "Vega-2 reflector")
	assert.NotContains(t, reply, "Kepler-9")
	var inspections int
	for _, e := range events.events {
		if e.Kind == agent.EventToolCall && e.ToolName == agent.MemoryInspectTool {
			require.False(t, e.ToolFailed)
			inspections++
		}
	}
	assert.Equal(t, 1, inspections, "the source-inspection tool actually ran")
	require.NoError(t, restarted.Close(ctx))
}

func TestConversation_UnsupportedQuestionHasNoEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	model := summaryScript(
		writebackStep(`{"reply":"I don't have a source for that.","note":null}`),
	)
	events := &conversationEvents{}
	rt, err := (&gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Tools: noTools()}).New(ctx, "a", events)
	require.NoError(t, err)
	reply, err := rt.Session.Run(ctx, "When is the lunar launch?")
	require.NoError(t, err)
	assert.Equal(t, "I don't have a source for that.", reply)
	var outcome agent.RecallOutcome
	for _, e := range events.events {
		if e.Kind == agent.EventRecall {
			outcome = e.RecallOutcome
		}
		assert.NotEqual(t, agent.MemoryInspectTool, e.ToolName)
	}
	assert.Equal(t, agent.RecallEmpty, outcome)
	require.NoError(t, rt.Close(ctx))
}

func TestConversation_QueuePreservesOriginalInboundID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	factory := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model {
		return summaryScript(
			writebackStep(`{"reply":"Noted.","note":null}`),
		)
	}, Tools: noTools()}
	queue, err := gateway.NewQueue(factory, "a", nil)
	require.NoError(t, err)
	id, err := queue.Enqueue(context.Background(), "sender", "Cedar is my favorite tea.")
	require.NoError(t, err)
	store, err := sqlite.Open(path)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		sources, err := store.Recent(context.Background(), 10)
		if err != nil {
			return false
		}
		for _, source := range sources {
			if source.ExternalID == id && source.Content == "Cedar is my favorite tea." {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, queue.Close())
	require.NoError(t, store.Close())
}

func TestConversation_RelatedCandidateDoesNotSupportMissingDate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	seed, err := sqlite.Open(path)
	require.NoError(t, err)
	require.NoError(t, seed.RecordTurn(ctx, []agent.SourceMessage{{Role: agent.RoleUser, Content: "The lunar mission uses the Aurora capsule."}, {Role: agent.RoleAssistant, Content: "Noted."}}, firstSummary{}))
	require.NoError(t, seed.Close())
	model := summaryScript(
		writebackStep(`{"reply":"I found the capsule name but no launch date in the available source.","note":null}`),
	)
	events := &conversationEvents{}
	rt, err := (&gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Embedder: wordEmbedder{}, Tools: noTools()}).New(ctx, "a", events)
	require.NoError(t, err)
	reply, err := rt.Session.Run(ctx, "When is the lunar mission launching?")
	require.NoError(t, err)
	assert.Contains(t, reply, "no launch date")
	var count int
	for _, e := range events.events {
		if e.Kind == agent.EventRecall {
			count = e.RecalledCount
		}
	}
	assert.Greater(t, count, 0, "a related candidate was retrieved, so this is an answer control, not an empty-search case")
	require.NoError(t, rt.Close(ctx))
}

// wordEmbedder counts each word into a hashed dimension, so texts sharing
// words are similar and texts sharing none are orthogonal.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vector := make([]float32, 4096)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		hash := fnv.New32a()
		_, _ = hash.Write([]byte(strings.Trim(word, ".,;:?!")))
		vector[hash.Sum32()%4096]++
	}
	return vector, nil
}

// firstSummary isolates store linking and search from synthesis quality.
type firstSummary struct{}

func (firstSummary) Summarize(_ context.Context, updates []string) (string, error) {
	return updates[0], nil
}

// summaryScript preserves each scenario's act replies while supplying summary fixtures.
func summaryScript(steps ...agent.ScriptedStep) agent.Model {
	return &summaryModel{inner: agent.NewScriptedModel(steps...)}
}

type summaryModel struct{ inner agent.Model }

func (m *summaryModel) Chat(ctx context.Context, messages []agent.Message, descriptors []tool.Descriptor) (agent.Response, error) {
	if len(descriptors) == 1 && descriptors[0].Name == "memory_summary" {
		var input struct {
			Updates []string `json:"updates"`
		}
		if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
			return agent.Response{}, err
		}
		summary := ""
		for _, update := range input.Updates {
			if strings.Contains(update, "preferred telescope") {
				summary = update
			}
		}
		if summary == "" {
			summary = input.Updates[0]
		}
		data, err := json.Marshal(map[string]string{"summary": summary})
		return agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: data}}}, err
	}
	return m.inner.Chat(ctx, messages, descriptors)
}

func checkpointSources(content string, count int) []agent.SourceMessage {
	var sources []agent.SourceMessage
	for i := 0; i < count; i++ {
		sources = append(sources, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	return sources
}

// leadingModel records the leading system message of every act call.
type leadingModel struct {
	inner   agent.Model
	leading []string
	results []string
}

func (m *leadingModel) Chat(ctx context.Context, messages []agent.Message, descriptors []tool.Descriptor) (agent.Response, error) {
	if len(descriptors) != 1 || descriptors[0].Name != "memory_summary" {
		m.leading = append(m.leading, messages[0].Content)
		if last := messages[len(messages)-1]; last.Role == agent.RoleTool {
			m.results = append(m.results, last.Content)
		}
	}
	return m.inner.Chat(ctx, messages, descriptors)
}

// TestConversation_NoteSurvivesCompactionAndRestart writes a note on the
// first turn, lets the two-message window drop that turn, and restarts: the
// note still leads the context, where the turn that stated it does not.
func TestConversation_NoteSurvivesCompactionAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	model := &leadingModel{inner: summaryScript(
		writebackStep(`{"reply":"Noted.","note":"Standing instruction: indent code with tabs."}`),
		writebackStep(`{"reply":"Filler answered.","note":null}`),
	)}
	f := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return model }, Embedder: wordEmbedder{}, Tools: noTools(), MaxHistoryMessages: 2}
	rt, err := f.New(ctx, "a", nil)
	require.NoError(t, err)
	_, err = rt.Session.Run(ctx, "Always indent code with tabs.")
	require.NoError(t, err)
	_, err = rt.Session.Run(ctx, "Unrelated filler question.")
	require.NoError(t, err)
	require.NoError(t, rt.Close(ctx))

	restartedModel := &leadingModel{inner: summaryScript(writebackStep(`{"reply":"Tabs.","note":null}`))}
	f.NewModel = func(string) agent.Model { return restartedModel }
	restarted, err := f.New(ctx, "a", nil)
	require.NoError(t, err)
	defer restarted.Close(ctx)
	_, err = restarted.Session.Run(ctx, "Write me a loop.")
	require.NoError(t, err)

	require.Len(t, model.leading, 2)
	assert.Contains(t, model.leading[0], "## Your note\n(empty)", "the control: no note before one is written")
	assert.Contains(t, model.leading[1], "## Your note\nStanding instruction: indent code with tabs.")
	require.Len(t, restartedModel.leading, 1)
	assert.Contains(t, restartedModel.leading[0], "## Your note\nStanding instruction: indent code with tabs.")
	for _, m := range restarted.Session.History() {
		assert.NotEqual(t, "Always indent code with tabs.", m.Content, "the window no longer holds the turn that stated the rule")
	}
}

// TestConversation_SearchReturnsTheEpisodeForTheModelsQuery searches with the
// query the model wrote and hands back the episode with its interval.
func TestConversation_SearchReturnsTheEpisodeForTheModelsQuery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memory.db")
	first := summaryScript(writebackStep(`{"reply":"Noted.","note":null}`))
	f := &gateway.Factory{MemoryPath: path, NewModel: func(string) agent.Model { return first }, Embedder: wordEmbedder{}, Tools: noTools(), MaxHistoryMessages: 2}
	rt, err := f.New(ctx, "a", nil)
	require.NoError(t, err)
	_, err = rt.Session.Run(agent.WithInboundSources(ctx, checkpointSources("My preferred telescope is the Kepler-9 refractor.", 3)), "Remember the telescope preference.")
	require.NoError(t, err)
	require.NoError(t, rt.Close(ctx))

	model := &leadingModel{inner: summaryScript(
		agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: agent.MemorySearchTool, Input: json.RawMessage(`{"query":"preferred telescope"}`)}}},
		writebackStep(`{"reply":"The Kepler-9 refractor.","note":null}`),
	)}
	f.NewModel = func(string) agent.Model { return model }
	restarted, err := f.New(ctx, "a", nil)
	require.NoError(t, err)
	defer restarted.Close(ctx)
	_, err = restarted.Session.Run(ctx, "Which one did I pick?")
	require.NoError(t, err)
	require.Len(t, model.results, 1)
	assert.Contains(t, model.results[0], "Kepler-9 refractor", "the search result carries the episode")
	assert.Contains(t, model.results[0], `"source_first":1`)
}

package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/internal/tool"
)

type synthesisModel struct {
	updates []string
}

func (m synthesisModel) Chat(_ context.Context, messages []agent.Message, _ []tool.Descriptor) (agent.Response, error) {
	var input struct {
		Updates []string
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		return agent.Response{}, err
	}
	if strings.Join(input.Updates, "|") != strings.Join(m.updates, "|") {
		return agent.Response{}, errors.New("batch evidence missing")
	}
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Current launch day: Friday. Favorite tea: cedar.","entities":["Cedar Tea House"]}`)}}}, nil
}

func TestConversationSummary_SynthesizesEpisodeWithCorrection(t *testing.T) {
	updates := []string{"Launch day: Tuesday. Favorite tea: cedar.", "Correction: the launch moved to Friday."}
	synthesis, err := (agentmemory.ConversationSummary{Model: synthesisModel{updates}}).Summarize(context.Background(), updates)
	require.NoError(t, err)
	assert.Contains(t, synthesis.Summary, "Friday")
	assert.Contains(t, synthesis.Summary, "cedar")
	assert.NotContains(t, synthesis.Summary, "Tuesday")
	assert.Equal(t, []string{"Cedar Tea House"}, synthesis.Entities)
}

func TestConversationSummary_RejectsUnusableModelResponse(t *testing.T) {
	tests := []struct {
		name  string
		calls []agent.ToolCall
		err   error
	}{
		{"valid control", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Current fact.","entities":["Vega"]}`)}}, nil},
		{"no entities", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Current fact."}`)}}, nil},
		{"entities not a list", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Current fact.","entities":"Vega"}`)}}, nil},
		{"no call", nil, nil},
		{"wrong call", []agent.ToolCall{{Name: "other", Input: json.RawMessage(`{"summary":"fact"}`)}}, nil},
		{"empty", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":" "}`)}}, nil},
		{"unknown field", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"fact","extra":true}`)}}, nil},
		{"trailing document", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"fact"} {}`)}}, nil},
		{"too long", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"` + strings.Repeat("x", 2001) + `"}`)}}, nil},
		{"model error", nil, errors.New("unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := agent.ScriptedStep{ToolCalls: tt.calls, Err: tt.err}
			model := agent.NewScriptedModel(step, step, step)
			result, err := (agentmemory.ConversationSummary{Model: model}).Summarize(context.Background(), []string{"new"})
			if tt.name == "valid control" {
				require.NoError(t, err)
				assert.Equal(t, agent.Synthesis{Summary: "Current fact.", Entities: []string{"Vega"}}, result)
			} else {
				require.Error(t, err)
				assert.NotErrorIs(t, err, agent.ErrScriptExhausted)
				assert.Zero(t, result)
			}
		})
	}
}

func TestConversationSummary_AsksForTheEntitiesInTheSameCall(t *testing.T) {
	model := &capturingModel{}
	synthesis, err := (agentmemory.ConversationSummary{Model: model}).Summarize(context.Background(), []string{"user: Every part for the Rodend build comes from Gebind Components."})
	require.NoError(t, err)
	assert.Equal(t, []string{"Rodend", "Gebind Components"}, synthesis.Entities)
	assert.Equal(t, 1, model.calls, "one call writes the summary and the entities")
	assert.True(t, strings.HasSuffix(model.prompt, "Call memory_summary with the episode summary and the names of the people, places, organizations, projects and other named things the batch mentions."))
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	require.NoError(t, json.Unmarshal(model.schema, &schema))
	assert.Equal(t, []string{"summary", "entities"}, schema.Required)
	assert.JSONEq(t, `{"type":"array","items":{"type":"string","minLength":1,"maxLength":200},"maxItems":50}`, string(schema.Properties["entities"]))
}

// capturingModel keeps the synthesis request it was sent and names two
// entities.
type capturingModel struct {
	calls  int
	prompt string
	schema json.RawMessage
}

func (m *capturingModel) Chat(_ context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.calls++
	m.prompt, m.schema = messages[0].Content, tools[0].InputSchema
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Rodend parts come from Gebind Components.","entities":["Rodend","Gebind Components"]}`)}}}, nil
}

// countingModel counts the calls it passes to the model it wraps.
type countingModel struct {
	agent.Model
	calls int
}

func (m *countingModel) Chat(ctx context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	m.calls++
	return m.Model.Chat(ctx, messages, tools)
}

// summaryCall is one valid memory_summary answer.
var summaryCall = agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Launch moved to Friday.","entities":["Friday launch"]}`)}}}

// skip is an answer in prose, without the memory_summary call.
var skip = agent.ScriptedStep{Content: "The launch moved to Friday."}

func TestConversationSummary_AsksAgainAfterAnAnswerWithoutTheCall(t *testing.T) {
	model := &countingModel{Model: agent.NewScriptedModel(skip, summaryCall, summaryCall)}
	synthesis, err := (agentmemory.ConversationSummary{Model: model}).Summarize(context.Background(), []string{"user: The launch moved to Friday."})
	require.NoError(t, err, "the second attempt's call is used")
	assert.Equal(t, agent.Synthesis{Summary: "Launch moved to Friday.", Entities: []string{"Friday launch"}}, synthesis)
	assert.Equal(t, 2, model.calls, "a usable answer ends the attempts")
}

func TestConversationSummary_AsksAgainAfterAMalformedCall(t *testing.T) {
	malformed := agent.ScriptedStep{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Launch moved to Friday."}`)}}}
	model := &countingModel{Model: agent.NewScriptedModel(malformed, summaryCall)}
	synthesis, err := (agentmemory.ConversationSummary{Model: model}).Summarize(context.Background(), []string{"user: The launch moved to Friday."})
	require.NoError(t, err, "a call without its entities is a failed attempt, not a final answer")
	assert.Equal(t, []string{"Friday launch"}, synthesis.Entities)
	assert.Equal(t, 2, model.calls)
}

func TestConversationSummary_DoesNotAskAgainAfterAModelError(t *testing.T) {
	model := &countingModel{Model: agent.NewScriptedModel(agent.ScriptedStep{Err: errors.New("unavailable")}, summaryCall)}
	_, err := (agentmemory.ConversationSummary{Model: model}).Summarize(context.Background(), []string{"user: The launch moved to Friday."})
	require.ErrorContains(t, err, "unavailable", "a failed call is not an answer, so it is not asked again")
	assert.Equal(t, 1, model.calls)
}

func TestRecordTurn_ThreeAnswersWithoutTheCallLeaveTheBatchPending(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenConversation(filepath.Join(t.TempDir(), "memory.db"), wordEmbedder{})
	require.NoError(t, err)
	defer store.Close()
	// The fourth step is the control: it is a valid call, so a fourth
	// attempt would have stored the episode.
	model := &countingModel{Model: agent.NewScriptedModel(skip, skip, skip, summaryCall)}
	summarizer := agentmemory.ConversationSummary{Model: model}
	err = store.RecordTurn(ctx, checkpoint("The launch moved to Friday."), summarizer)
	require.ErrorContains(t, err, "synthesize conversation: model must call memory_summary exactly once")
	assert.Equal(t, 3, model.calls)
	pending, err := store.PendingSources(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, 4, "the batch stays pending for the next completion")
	episodes, err := store.RecentMid(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, episodes)

	require.NoError(t, store.Accumulate(ctx, summarizer), "the next completion retries the batch")
	episodes, err = store.RecentMid(ctx, 10)
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Equal(t, "Launch moved to Friday.", episodes[0].Summary)
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

func checkpoint(content string) []agent.SourceMessage {
	var messages []agent.SourceMessage
	for i := 0; i < 4; i++ {
		messages = append(messages, agent.SourceMessage{Role: agent.RoleUser, Content: content})
	}
	return messages
}

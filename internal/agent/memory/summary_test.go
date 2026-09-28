package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
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
			model := agent.NewScriptedModel(agent.ScriptedStep{ToolCalls: tt.calls, Err: tt.err})
			result, err := (agentmemory.ConversationSummary{Model: model}).Summarize(context.Background(), []string{"new"})
			if tt.name == "valid control" {
				require.NoError(t, err)
				assert.Equal(t, agent.Synthesis{Summary: "Current fact.", Entities: []string{"Vega"}}, result)
			} else {
				require.Error(t, err)
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

package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
	"github.com/garamsh/gagent/internal/tool"
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
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Current launch day: Friday. Favorite tea: cedar."}`)}}}, nil
}

func TestConversationSummary_SynthesizesEpisodeWithCorrection(t *testing.T) {
	updates := []string{"Launch day: Tuesday. Favorite tea: cedar.", "Correction: the launch moved to Friday."}
	summary, err := (agentmemory.ConversationSummary{Model: synthesisModel{updates}}).Summarize(context.Background(), updates)
	require.NoError(t, err)
	assert.Contains(t, summary, "Friday")
	assert.Contains(t, summary, "cedar")
	assert.NotContains(t, summary, "Tuesday")
}

func TestConversationSummary_RejectsUnusableModelResponse(t *testing.T) {
	tests := []struct {
		name  string
		calls []agent.ToolCall
		err   error
	}{
		{"valid control", []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(`{"summary":"Current fact."}`)}}, nil},
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
				assert.Equal(t, "Current fact.", result)
			} else {
				require.Error(t, err)
				assert.Empty(t, result)
			}
		})
	}
}

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/tool"
)

// capture records the one request it is sent and answers with a fixed call.
type capture struct {
	messages []agent.Message
	tools    []tool.Descriptor
	answer   string
}

func (c *capture) Chat(_ context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	c.messages, c.tools = messages, tools
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(c.answer)}}}, nil
}

func TestSynthesizeFacts_SendsProductionsRequestWithTheFactsAdded(t *testing.T) {
	updates := []string{"user: My horse is called Wouvull.", "assistant: Noted."}
	production := &capture{answer: `{"summary":"s"}`}
	_, err := agentmemory.ConversationSummary{Model: production}.Summarize(context.Background(), updates)
	require.NoError(t, err)
	extended := &capture{answer: `{"summary":"s","facts":[{"kind":"user","text":"The user's horse is Wouvull."}]}`}
	facts, err := synthesizeFacts(context.Background(), extended, updates, 1)
	require.NoError(t, err)
	assert.Equal(t, []Fact{{Kind: "user", Text: "The user's horse is Wouvull."}}, facts)

	prod, ext := production.messages[0].Content, extended.messages[0].Content
	cut := strings.LastIndex(prod, " Call memory_summary")
	require.Positive(t, cut)
	assert.Equal(t, prod[:cut], ext[:cut], "the prompt is production's up to its last sentence")
	assert.Equal(t, production.messages[1], extended.messages[1], "the batch is sent as production sends it")
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	require.NoError(t, json.Unmarshal(extended.tools[0].InputSchema, &schema))
	assert.Contains(t, schema.Properties, "facts")
	assert.ElementsMatch(t, []string{"summary", "facts"}, schema.Required)
	assert.Equal(t, production.tools[0].Name, extended.tools[0].Name)
}

func TestSynthesizeFacts_VariesOnlyARetriedRequest(t *testing.T) {
	first, retry := &capture{answer: `{"summary":"s","facts":[]}`}, &capture{answer: `{"summary":"s","facts":[]}`}
	_, err := synthesizeFacts(context.Background(), first, []string{"user: hi"}, 1)
	require.NoError(t, err)
	_, err = synthesizeFacts(context.Background(), retry, []string{"user: hi"}, 2)
	require.NoError(t, err)
	assert.Equal(t, factPrompt, first.messages[0].Content)
	assert.NotEqual(t, first.messages[0].Content, retry.messages[0].Content)
}

func TestParseFacts_RefusesWhatTheSchemaBounds(t *testing.T) {
	for name, input := range map[string]string{
		"missing facts":  `{"summary":"s"}`,
		"unknown kind":   `{"summary":"s","facts":[{"kind":"place","text":"x"}]}`,
		"empty text":     `{"summary":"s","facts":[{"kind":"user","text":" "}]}`,
		"too long":       `{"summary":"s","facts":[{"kind":"user","text":"` + strings.Repeat("x", 201) + `"}]}`,
		"too many":       `{"summary":"s","facts":[` + strings.TrimSuffix(strings.Repeat(`{"kind":"user","text":"x"},`, 6), ",") + `]}`,
		"blank summary":  `{"summary":" ","facts":[]}`,
		"a string array": `{"summary":"s","facts":"none"}`,
	} {
		_, err := parseFacts(json.RawMessage(input))
		assert.Error(t, err, name)
	}
	// Control: the bounds themselves are accepted.
	facts, err := parseFacts(json.RawMessage(`{"summary":"s","facts":[` + strings.TrimSuffix(strings.Repeat(`{"kind":"other","text":"`+strings.Repeat("y", 200)+`"},`, 5), ",") + `]}`))
	require.NoError(t, err)
	assert.Len(t, facts, 5)
}

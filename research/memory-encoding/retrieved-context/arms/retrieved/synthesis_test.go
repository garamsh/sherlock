package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/tool"
)

// capture records the request it is sent and answers with one tool call.
type capture struct {
	messages []agent.Message
	tools    []tool.Descriptor
	name     string
	input    string
}

func (c *capture) Chat(_ context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	c.messages, c.tools = messages, tools
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: c.name, Input: json.RawMessage(c.input)}}}, nil
}

var found = retrieved{
	Messages: []vec{{ID: 3, First: 3, Last: 3, Text: "user: Our account at Zevai Components is looked after by Chloe Traike."}},
	Mids:     []vec{{ID: 7, First: 25, Last: 28, Text: "Chloe Traike took over the Zevai account."}},
}

var updates = []string{"user: Chloe Traike answers on extension 6200.", "assistant: Got it."}

func TestSynthesizeRW_SendsTheProductionRequestWithTheRetrievedContextAdded(t *testing.T) {
	ctx := context.Background()
	production := &capture{name: "memory_summary", input: `{"summary":"s"}`}
	_, err := agentmemory.ConversationSummary{Model: production}.Summarize(ctx, updates)
	require.NoError(t, err)
	rw := &capture{name: "memory_summary", input: `{"summary":"Chloe Traike, who looks after Zevai Components, is on 6200."}`}
	got, err := synthesizeRW(ctx, rw, found, updates, 1)
	require.NoError(t, err)
	assert.Equal(t, "Chloe Traike, who looks after Zevai Components, is on 6200.", got)

	last := " Call memory_summary with the episode summary."
	require.True(t, strings.HasSuffix(production.messages[0].Content, last))
	assert.Equal(t, strings.TrimSuffix(production.messages[0].Content, last)+retrievedSentence+last, rw.messages[0].Content,
		"the production prompt with one sentence before its last")

	var prodInput map[string][]string
	require.NoError(t, json.Unmarshal([]byte(production.messages[1].Content), &prodInput))
	var rwInput struct {
		RetrievedMessages []string      `json:"retrieved_messages"`
		RetrievedMemories []memoryInput `json:"retrieved_memories"`
		Updates           []string      `json:"updates"`
	}
	require.NoError(t, json.Unmarshal([]byte(rw.messages[1].Content), &rwInput))
	assert.Equal(t, prodInput["updates"], rwInput.Updates, "the batch is sent as production sends it")
	assert.Equal(t, []string{found.Messages[0].Text}, rwInput.RetrievedMessages)
	assert.Equal(t, []memoryInput{{Mid: 7, Summary: found.Mids[0].Text}}, rwInput.RetrievedMemories)

	assert.Equal(t, production.tools, rw.tools, "the production schema, unchanged, so the proxy ledgers and caches it as a synthesis")
}

func TestSynthesizeRW_VariesARetriedRequest(t *testing.T) {
	ctx := context.Background()
	first, second := &capture{name: "memory_summary", input: `{"summary":"s"}`}, &capture{name: "memory_summary", input: `{"summary":"s"}`}
	_, err := synthesizeRW(ctx, first, found, updates, 1)
	require.NoError(t, err)
	_, err = synthesizeRW(ctx, second, found, updates, 2)
	require.NoError(t, err)
	assert.Equal(t, rwPrompt, first.messages[0].Content, "a first attempt is the plain request")
	assert.NotEqual(t, first.messages[0].Content, second.messages[0].Content, "the proxy's cache cannot answer a retry")
}

func TestParseSummary_RejectsWhatProductionRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", `{"summary":" "}`},
		{"too long", fmt.Sprintf(`{"summary":%q}`, strings.Repeat("x", maxSummary+1))},
		{"unknown field", `{"summary":"s","context":"c"}`},
		{"two objects", `{"summary":"s"}{"summary":"t"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSummary(json.RawMessage(tt.in))
			assert.Error(t, err)
		})
	}
	got, err := parseSummary(json.RawMessage(fmt.Sprintf(`{"summary":%q}`, strings.Repeat("x", maxSummary))))
	require.NoError(t, err, "a summary at the bound is an answer")
	assert.Len(t, got, maxSummary)
}

func TestSynthesizeRelated_SendsTheSameContextThroughItsOwnSchema(t *testing.T) {
	ctx := context.Background()
	c := &capture{name: "memory_related", input: `{"entities":["Zevai Components","Chloe Traike"],"mids":[7]}`}
	got, err := synthesizeRelated(ctx, c, found, updates, 1)
	require.NoError(t, err)
	assert.Equal(t, related{Entities: []string{"Zevai Components", "Chloe Traike"}, Mids: []int64{7}}, got)
	assert.Equal(t, relatedPrompt, c.messages[0].Content)
	want, err := input(found, updates)
	require.NoError(t, err)
	assert.Equal(t, want, c.messages[1].Content, "the same input RW is sent")
	require.Len(t, c.tools, 1)
	assert.Equal(t, "memory_related", c.tools[0].Name, "never memory_summary: the call writes no summary")
	assert.JSONEq(t, relatedSchema, string(c.tools[0].InputSchema))

	wrong := &capture{name: "memory_summary", input: `{"summary":"s"}`}
	_, err = synthesizeRelated(ctx, wrong, found, updates, 1)
	assert.Error(t, err, "an answer through another tool is no answer")
}

func TestParseRelated_BoundsTheLineAndKeepsOnlyWhatItWasSent(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"no mids", `{"entities":[]}`},
		{"no entities", `{"mids":[]}`},
		{"too many names", `{"entities":["a","b","c","d","e","f","g"],"mids":[]}`},
		{"too many mids", `{"entities":[],"mids":[1,2,3,4,5]}`},
		{"long name", fmt.Sprintf(`{"entities":[%q],"mids":[]}`, strings.Repeat("x", maxEntityLen+1))},
		{"empty name", `{"entities":[" "],"mids":[]}`},
		{"unknown field", `{"entities":[],"mids":[],"summary":"s"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseRelated(json.RawMessage(tt.in), found, updates)
			assert.Error(t, err)
		})
	}
	got, err := parseRelated(json.RawMessage(`{"entities":["zevai components","Oscar Goudre","Chloe Traike","Chloe Traike"],"mids":[7,9,7]}`), found, updates)
	require.NoError(t, err)
	assert.Equal(t, []string{"zevai components", "Chloe Traike"}, got.Entities, "a name written nowhere it was sent is dropped")
	assert.Equal(t, []int64{7}, got.Mids, "a mid it was not sent is dropped")
	assert.Equal(t, []string{"entity: Oscar Goudre", "mid: 9"}, got.Dropped)

	empty, err := parseRelated(json.RawMessage(`{"entities":[],"mids":[]}`), found, updates)
	require.NoError(t, err, "naming nothing is an answer")
	assert.Equal(t, related{Entities: []string{}, Mids: []int64{}}, empty)
}

func TestRelatedLine_NamesOnly(t *testing.T) {
	assert.Equal(t, "Related to: Zevai Components, Chloe Traike; earlier mids 7, 12.",
		relatedLine(related{Entities: []string{"Zevai Components", "Chloe Traike"}, Mids: []int64{7, 12}}))
	assert.Equal(t, "Related to: Zevai Components.", relatedLine(related{Entities: []string{"Zevai Components"}}))
	assert.Equal(t, "earlier mids 7.", relatedLine(related{Mids: []int64{7}}))
	assert.Equal(t, "", relatedLine(related{Entities: []string{}, Mids: []int64{}}), "no line when nothing relates")
	full := related{Mids: []int64{1, 2, 3, 4}}
	for range maxEntities {
		full.Entities = append(full.Entities, strings.Repeat("x", maxEntityLen))
	}
	assert.LessOrEqual(t, len(relatedLine(full)), 450, "the line is bounded by the schema")
}

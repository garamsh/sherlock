package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func synthesisResponse(summary string) []byte {
	args, _ := json.Marshal(map[string]string{"summary": summary})
	call, _ := json.Marshal(map[string]any{"id": "c", "type": "function", "function": map[string]string{"name": "memory_summary", "arguments": string(args)}})
	return []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[` + string(call) + `]}}]}`)
}

func TestSummarized_AcceptsOnlyWhatTheProductionSummarizerAccepts(t *testing.T) {
	assert.True(t, summarized(synthesisResponse(strings.Repeat("a", 2000))), "the control: a summary at the bound")
	assert.False(t, summarized(synthesisResponse(strings.Repeat("a", 2019))), "a summary past 2,000 characters, as in window g4-0263")
	assert.False(t, summarized(synthesisResponse("   ")), "a blank summary")
	assert.False(t, summarized([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"memory_summary","arguments":"not json"}}]}}]}`)))
	assert.False(t, summarized([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"memory_write","arguments":"{}"}}]}}]}`)))
}

func entityResponse(entities string) []byte {
	args := `{"summary":"s","entities":` + entities + `}`
	call, _ := json.Marshal(map[string]any{"id": "c", "type": "function", "function": map[string]string{"name": "memory_summary", "arguments": args}})
	return []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[` + string(call) + `]}}]}`)
}

func TestSummarized_RefusesEntitiesThatAreNotAnArrayOfStrings(t *testing.T) {
	assert.True(t, summarized(entityResponse(`["Rodend","Gebind Components"]`)), "the control: arm L's extended answer")
	assert.True(t, summarized(entityResponse(`[]`)), "the control: a batch naming nothing")
	assert.False(t, summarized(entityResponse(`"Rodend, Gebind Components"`)), "entities as a string, as in episode b4-00128")
	assert.False(t, summarized(entityResponse(`null`)))
	assert.False(t, summarized(entityResponse(`[1,2]`)))
}

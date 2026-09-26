package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/agent"
	agentmemory "github.com/garamsh/sherlock/internal/agent/memory"
	"github.com/garamsh/sherlock/internal/tool"
)

// capture records the request it is sent and answers with one tool call.
type capture struct {
	messages []agent.Message
	tools    []tool.Descriptor
	input    string
}

func (c *capture) Chat(_ context.Context, messages []agent.Message, tools []tool.Descriptor) (agent.Response, error) {
	c.messages, c.tools = messages, tools
	return agent.Response{ToolCalls: []agent.ToolCall{{Name: "memory_summary", Input: json.RawMessage(c.input)}}}, nil
}

func TestSynthesizeEntities_SendsTheProductionRequestWithEntitiesAdded(t *testing.T) {
	ctx := context.Background()
	updates := []string{"user: Every part for the Rodend build is ordered from Gebind Components.", "assistant: Noted."}
	production := &capture{input: `{"summary":"s"}`}
	_, err := agentmemory.ConversationSummary{Model: production}.Summarize(ctx, updates)
	require.NoError(t, err)
	extended := &capture{input: `{"summary":"s","entities":["Rodend","Gebind Components"]}`}
	got, err := synthesizeEntities(ctx, extended, updates)
	require.NoError(t, err)
	assert.Equal(t, []string{"Rodend", "Gebind Components"}, got)

	asked := " and the names of the people, places, organizations, projects and other named things the batch mentions."
	assert.Equal(t, strings.TrimSuffix(production.messages[0].Content, ".")+asked, extended.messages[0].Content)
	assert.Equal(t, production.messages[1], extended.messages[1], "the batch is sent as production sends it")
	var prodSchema, extSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(production.tools[0].InputSchema, &prodSchema))
	require.NoError(t, json.Unmarshal(extended.tools[0].InputSchema, &extSchema))
	assert.Equal(t, prodSchema.Properties["summary"], extSchema.Properties["summary"])
	assert.Contains(t, extSchema.Properties, "entities")
	assert.Equal(t, production.tools[0].Name, extended.tools[0].Name, "the proxy ledgers it as a synthesis call")
}

func TestSynthesizeEntities_RejectsAResponseProductionWouldRejectOrWithoutEntities(t *testing.T) {
	ctx := context.Background()
	for name, input := range map[string]string{
		"no entities":   `{"summary":"s"}`,
		"empty summary": `{"summary":" ","entities":["A"]}`,
		"unknown field": `{"summary":"s","entities":["A"],"extra":1}`,
	} {
		_, err := synthesizeEntities(ctx, &capture{input: input}, []string{"user: hi"})
		assert.Error(t, err, name)
	}
	got, err := synthesizeEntities(ctx, &capture{input: `{"summary":"s","entities":[]}`}, []string{"user: hi"})
	require.NoError(t, err, "the control: an empty list is an answer")
	assert.Empty(t, got)
}

// fakeProxy answers every chat request with the same entities and counts
// the calls and the routes they came by.
func fakeProxy(t *testing.T, calls *atomic.Int32, routes chan<- string) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/budget" {
			fmt.Fprint(w, `{"paused":false}`)
			return
		}
		calls.Add(1)
		routes <- r.URL.Path
		args, _ := json.Marshal(`{"summary":"s","entities":["Rodend"]}`)
		fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"1","type":"function","function":{"name":"memory_summary","arguments":%s}}]}}]}`, args)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRecordEntities_SendsOnlyUnrecordedBatchesAndRecordsThem(t *testing.T) {
	var calls atomic.Int32
	routes := make(chan string, 10)
	server := fakeProxy(t, &calls, routes)
	var messages []study.Message
	for i := 1; i <= 12; i++ {
		messages = append(messages, study.Message{ID: int64(i), Role: "user", Content: fmt.Sprintf("message %d", i)})
	}
	env := &study.Env{Corpus: &study.Corpus{Base: messages}, Dir: t.TempDir(), Proxy: server.URL, Arm: "L"}
	path := entitiesPath(env, 4)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"first":5,"last":8,"entities":["Gebind"]}`+"\n"), 0o644))

	got, err := recordEntities(context.Background(), env, 4)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "batches 1 and 3 are sent; batch 2 is recorded")
	assert.Equal(t, map[int64][]string{1: {"Rodend"}, 5: {"Gebind"}, 9: {"Rodend"}}, got)
	assert.ElementsMatch(t, []string{"/L/b4-00001/entities-b4/v1/chat/completions", "/L/b4-00003/entities-b4/v1/chat/completions"}, []string{<-routes, <-routes})

	again, err := recordEntities(context.Background(), env, 4)
	require.NoError(t, err)
	assert.Equal(t, got, again)
	assert.Equal(t, int32(2), calls.Load(), "a complete recording makes no call")
}

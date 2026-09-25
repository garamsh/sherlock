package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
)

func TestLedger_RefusesPastTheCeilingAndSettlesAtUsage(t *testing.T) {
	l := &ledger{ceiling: 1.0, spent: 0.5}
	require.NoError(t, l.reserve(0.4), "the control: a request within the ceiling is reserved")
	err := l.reserve(0.2)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errBudget))
	l.settle(0.4, 0.1)
	spent, reserved := l.state()
	assert.InDelta(t, 0.6, spent, 1e-9)
	assert.InDelta(t, 0, reserved, 1e-9)
	require.NoError(t, l.reserve(0.2), "settling at usage frees the rest of the reservation")
}

func TestReservation_CoversInputBytesAndTheWholeOutputBound(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 3000)
	assert.InDelta(t, 1000*inputUSDPerToken+4096*outputUSDPerToken, reservation(body, 4096), 1e-12)
	assert.InDelta(t, 100*inputUSDPerToken+10*outputUSDPerToken, cost(100, 10), 1e-12)
}

func TestRequestKind_NamesTheReservedCall(t *testing.T) {
	for body, want := range map[string]string{
		`{"tools":[{"function":{"name":"memory_summary"}}]}`:                                    "synthesis",
		`{"tools":[{"function":{"name":"memory_understand"}}]}`:                                 "understand",
		`{"tools":[{"function":{"name":"memory_write"}},{"function":{"name":"message_send"}}]}`: "act",
		`{"tools":[{"function":{"name":"memory_write"}}]}`:                                      "act",
	} {
		assert.Equal(t, want, requestKind([]byte(body)), body)
	}
}

func TestWithMaxTokens_AddsTheBoundOnlyWhenAbsent(t *testing.T) {
	out, err := withMaxTokens([]byte(`{"model":"m"}`), 4096)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"m","max_tokens":4096}`, string(out))
	out, err = withMaxTokens([]byte(`{"model":"m","max_tokens":10}`), 4096)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"m","max_tokens":10}`, string(out))
}

func TestRoute_ReadsArmEpisodeAndPhase(t *testing.T) {
	arm, episode, phase, ok := route("/A/G-01/probe/v1/chat/completions")
	require.True(t, ok)
	assert.Equal(t, []string{"A", "G-01", "probe"}, []string{arm, episode, phase})
	_, _, _, ok = route("/A/G-01/v1/chat/completions")
	assert.False(t, ok)
}

// upstream answers like the provider, counting requests; the first
// failures answers answer with the given status.
func upstream(t *testing.T, failures int, status int) (*httptest.Server, *atomic.Int32, *[]string) {
	var n atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if int(n.Add(1)) <= failures {
			http.Error(w, "busy", status)
			return
		}
		_, _ = w.Write([]byte(`{"id":"r1","model":"MiniMax-M2","choices":[{"finish_reason":"stop","message":{"content":"ok"}}],"usage":{"prompt_tokens":1000,"completion_tokens":100}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &bodies
}

func newTestProxy(t *testing.T, url string, ceiling float64) (*proxy, *bytes.Buffer) {
	var log bytes.Buffer
	return &proxy{upstream: url, key: "k", ledger: &ledger{ceiling: ceiling}, client: http.DefaultClient,
		log: &log, cache: map[string][]byte{}, inflight: map[string]chan struct{}{}, cacheLog: io.Discard}, &log
}

func post(p *proxy, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return w
}

func TestProxy_SynthesisIsAnsweredOnceThenFromTheCache(t *testing.T) {
	srv, n, bodies := upstream(t, 0, 0)
	p, log := newTestProxy(t, srv.URL, 1)
	synthesis := `{"messages":[{"role":"user","content":"batch"}],"tools":[{"function":{"name":"memory_summary"}}]}`
	act := `{"messages":[{"role":"user","content":"batch"}],"tools":[{"function":{"name":"memory_write"}},{"function":{"name":"message_send"}}]}`
	for range 2 {
		require.Equal(t, http.StatusOK, post(p, "/S/G-01/seed/v1/chat/completions", synthesis).Code)
		require.Equal(t, http.StatusOK, post(p, "/S/G-01/probe/v1/chat/completions", act).Code)
	}
	assert.Equal(t, int32(3), n.Load(), "the repeated synthesis is cached; the repeated act call is not")
	assert.Contains(t, (*bodies)[0], `"max_tokens":2048`)
	assert.Contains(t, (*bodies)[1], `"max_tokens":4096`)
	assert.Equal(t, 4, strings.Count(log.String(), "\n"), "every request is in the ledger, the cached one too")
	assert.Contains(t, log.String(), `"cached":true`)
	spent, _ := p.ledger.state()
	assert.InDelta(t, 3*cost(1000, 100), spent, 1e-12)
}

func TestProxy_RetriesARateLimitAndRefusesPastTheCeiling(t *testing.T) {
	srv, n, _ := upstream(t, 1, http.StatusTooManyRequests)
	p, _ := newTestProxy(t, srv.URL, 1)
	act := `{"messages":[],"tools":[]}`
	require.Equal(t, http.StatusOK, post(p, "/A/K-01/probe/v1/chat/completions", act).Code)
	assert.Equal(t, int32(2), n.Load(), "one rate limit, one retry")

	tight, _ := newTestProxy(t, srv.URL, reservation([]byte(act), 4096)/2)
	w := post(tight, "/A/K-01/probe/v1/chat/completions", act)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
	assert.Equal(t, int32(2), n.Load(), "a refused request never reaches the provider")
}

func TestReached_CountsContextNotTheModelsOwnOutput(t *testing.T) {
	input := "[msg-G-01-4] owner: What budget did I give you for it?"
	withKey := exchange{Tools: []string{"memory_write"}, Messages: []agent.Message{
		{Role: agent.RoleSystem, Content: "instructions"},
		{Role: agent.RoleUser, Content: input},
		{Role: agent.RoleAssistant, Content: "I guess €11,000", ToolCalls: []agent.ToolCall{{Name: "memory_search"}}},
		{Role: agent.RoleTool, Content: `[{"summary":"budget for Mara's offsite is €11,000"}]`},
	}}
	assert.True(t, reached([]exchange{withKey}, input, []string{"€11,000"}), "a tool result after the input counts")

	ownOutput := exchange{Tools: []string{"memory_write"}, Messages: withKey.Messages[:3]}
	assert.False(t, reached([]exchange{ownOutput}, input, []string{"€11,000"}), "the model's own guess does not")

	window := exchange{Tools: []string{"memory_write"}, Messages: []agent.Message{
		{Role: agent.RoleAssistant, Content: "Got it: the budget is €11,000."},
		{Role: agent.RoleUser, Content: input},
	}}
	assert.True(t, reached([]exchange{window}, input, []string{"€11,000"}), "an earlier turn's message in the window counts")
	synthesis := exchange{Tools: []string{"memory_summary"}, Messages: window.Messages}
	assert.False(t, reached([]exchange{synthesis}, input, []string{"€11,000"}), "a synthesis call is not the act model's context")
}

func TestWorkspace_KeepsFilesAndSendsPerTake(t *testing.T) {
	ws := newWorkspace(nil)
	ctx := context.Background()
	_, err := ws.Call(ctx, "file_write", json.RawMessage(`{"path":"./notes/../rsvp.txt","content":"Alba\nBram"}`))
	require.NoError(t, err)
	_, err = ws.Call(ctx, "message_send", json.RawMessage(`{"channel":"http","target":"owner","content":"done"}`))
	require.NoError(t, err)
	out, err := ws.Call(ctx, "file_read", json.RawMessage(`{"path":"rsvp.txt"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"Alba\nBram"}`, string(out))
	sent, written := ws.take()
	assert.Equal(t, []string{"done"}, sent)
	assert.Equal(t, map[string]string{"rsvp.txt": "Alba\nBram"}, written)
	sent, written = ws.take()
	assert.Empty(t, sent)
	assert.Empty(t, written, "a take returns only what came after the previous one")
	_, err = ws.Call(ctx, "shell_exec", json.RawMessage(`{}`))
	assert.ErrorContains(t, err, "not found")
}

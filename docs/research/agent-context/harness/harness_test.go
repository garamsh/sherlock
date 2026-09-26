package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
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
		_, _ = w.Write([]byte(`{"id":"r1","model":"MiniMax-M2","choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"function":{"name":"memory_summary","arguments":"{}"}}]}}],"usage":{"prompt_tokens":1000,"completion_tokens":100}}`))
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

func TestProxy_ReRequestsASynthesisTheSummarizerWouldRefuse(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"a summary in prose"}}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"memory_summary","arguments":"{}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`))
	}))
	t.Cleanup(srv.Close)
	p, log := newTestProxy(t, srv.URL, 1)
	synthesis := `{"messages":[],"tools":[{"function":{"name":"memory_summary"}}]}`
	w := post(p, "/S/E-01/seed/v1/chat/completions", synthesis)
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, summarized(w.Body.Bytes()), "the caller gets the usable answer")
	assert.Equal(t, int32(2), n.Load())
	assert.Contains(t, log.String(), "synthesis without exactly one memory_summary call")
	post(p, "/S/E-01/seed/v1/chat/completions", synthesis)
	assert.Equal(t, int32(2), n.Load(), "only the usable answer was cached")
	assert.False(t, summarized([]byte(`{"choices":[{"message":{"content":"prose"}}]}`)), "the control: prose is not a summary")
}

// TestContainsAny_MatchesAsTheScorerDoes pins the examples score_test.py
// pins for states, so reach and application are judged alike.
func TestContainsAny_MatchesAsTheScorerDoes(t *testing.T) {
	for _, tc := range []struct {
		text, key string
		want      bool
	}{
		{"The budget is 14,100 euros.", "€14,100", true},
		{"The budget is 141,000 euros.", "€14,100", false},
		{"Book the Arden hall.", "the Arden Hall", true},
		{"Book the Arden loft.", "the Arden Hall", false},
		{"March 22nd at 2:45 PM", "22 March at 14:45", true},
		{"March 22nd at 3:45 PM", "22 March at 14:45", false},
		{"June 2nd", "2 June", true},
		{"June 12th", "2 June", false},
		{"User stays at Tsuki House in Kyoto.", "the Tsuki House", true},
		{"Kit size is M8.", "size M8", true},
	} {
		assert.Equal(t, tc.want, containsAny(tc.text, []string{tc.key}), "%q in %q", tc.key, tc.text)
	}
}

// wordEmbedder counts each word into a hashed dimension, so texts sharing
// words are similar and texts sharing none are orthogonal.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 512)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := 0
		for _, c := range strings.Trim(w, ".,:;?!") {
			h = h*31 + int(c)
		}
		v[(h%512+512)%512]++
	}
	return v, nil
}

// firstUpdate summarizes a batch by its first message, so a test controls
// what each mid's summary says.
type firstUpdate struct{}

func (firstUpdate) Summarize(_ context.Context, updates []string) (string, error) {
	return updates[0], nil
}

// oracleStore records the given batches of four user messages each and
// returns the store's path.
func oracleStore(t *testing.T, batches [][4]string) string {
	t.Helper()
	path := t.TempDir() + "/memory.db"
	store, err := sqlite.OpenConversation(path, wordEmbedder{})
	require.NoError(t, err)
	for _, b := range batches {
		for _, m := range b {
			require.NoError(t, store.RecordTurn(context.Background(), []agent.SourceMessage{{Role: agent.RoleUser, Content: m}}, firstUpdate{}))
		}
	}
	require.NoError(t, store.Close())
	return path
}

func TestOracle_ReachesTheSourceThroughItsMidsInterval(t *testing.T) {
	statement := "The budget for Mara's offsite is 4,100 euros."
	r := &runner{embedder: wordEmbedder{}}
	ctx := context.Background()

	reached := oracleStore(t, [][4]string{{statement, "ok", "fine", "sure"}, {"weather talk", "rain", "sun", "wind"}})
	check, err := r.oracle(ctx, reached, statement, []string{"4,100"})
	require.NoError(t, err)
	assert.True(t, check.Reached, "the control: a mid holding the source ranks first")
	assert.Equal(t, []int64{1}, check.Sources)
	assert.Equal(t, 1, check.Rank)
	assert.Empty(t, check.Class)

	// Six mids echo the statement's words ahead of the one holding it,
	// whose summary is about something else.
	var batches [][4]string
	for i := range 6 {
		batches = append(batches, [4]string{fmt.Sprintf("The budget for Mara's offsite is a question %d", i), "a", "b", "c"})
	}
	batches = append(batches, [4]string{"garden hose repair", "hmm", statement, "noted"})
	reworded := oracleStore(t, batches)
	check, err = r.oracle(ctx, reworded, statement, []string{"4,100"})
	require.NoError(t, err)
	assert.False(t, check.Reached)
	assert.Equal(t, 7, check.Rank)
	assert.Equal(t, "reworded summary", check.Class, "the holding mid's summary never states the key")

	batches[6] = [4]string{"garden hose 4,100 repair", "hmm", statement, "noted"}
	ranking := oracleStore(t, batches)
	check, err = r.oracle(ctx, ranking, statement, []string{"4,100"})
	require.NoError(t, err)
	assert.False(t, check.Reached)
	assert.Equal(t, "ranking miss", check.Class, "the summary states the key and still ranks below five")

	_, err = r.oracle(ctx, ranking, "a sentence nobody said", []string{"x"})
	assert.Error(t, err, "an evidence statement missing from the store is a harness error, not a miss")
}

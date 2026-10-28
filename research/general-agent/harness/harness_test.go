package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent/openai"
)

const repoRoot = "../../.."

// fakeUpstream answers as OpenRouter would: a synthesis with one
// memory_summary call, anything else with the terminal call. It keeps
// every request body it received.
type fakeUpstream struct {
	mu       sync.Mutex
	requests []map[string]json.RawMessage
}

func (u *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(body, &doc)
	u.mu.Lock()
	u.requests = append(u.requests, doc)
	u.mu.Unlock()
	name, args := "memory_write", `{"reply":"- one\n- two\n- three"}`
	if requestKind(body) == "synthesis" {
		name, args = "memory_summary", `{"summary":"A batch of conversation.","entities":[]}`
	}
	resp := map[string]any{
		"id": "gen-1", "model": "pinned/model", "provider": "Pinned",
		"choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{
			"role": "assistant", "content": "",
			"tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": name, "arguments": args}}},
		}}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 10, "cost": 0.0001},
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// wordEmbedder gives each text a vector from its words, offline.
type wordEmbedder struct{}

func (wordEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 64)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		v[len(w)%64]++
	}
	v[0]++
	return v, nil
}

func startProxy(t *testing.T, upstream string, ceiling float64) (*proxy, string) {
	t.Helper()
	p, closeLogs, err := newProxy(proxyConfig{upstream: upstream, key: "test-key", dir: t.TempDir(), ceiling: ceiling,
		model: "pinned/model", provider: "Pinned", price: price{input: 1e-8, output: 1e-8}})
	require.NoError(t, err)
	t.Cleanup(closeLogs)
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return p, srv.URL
}

func readResults(t *testing.T, path string) []result {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var out []result
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		var r result
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &r))
		out = append(out, r)
	}
	return out
}

func runFamilies(t *testing.T, proxyURL string, window int, ids ...string) []result {
	t.Helper()
	r, err := newRunner(runnerConfig{arm: "X", corpusPath: filepath.Join(repoRoot, "research/general-agent/corpus.json"),
		proxy: proxyURL, model: "requested/model", toolsDir: filepath.Join(repoRoot, "tools"), window: window, embedder: wordEmbedder{}})
	require.NoError(t, err)
	out := t.TempDir()
	only := map[string]bool{}
	for _, id := range ids {
		only[id] = true
	}
	require.NoError(t, r.runAll(out, only, 2))
	return readResults(t, filepath.Join(out, "results.jsonl"))
}

// TestRun_EveryLiveStepRunsThroughTheProductionRuntime runs one primary
// family of each kind against a stand-in upstream through the real proxy.
// Every live step is recorded, the corrections have left the window at
// every probe, and the proxy sent the pinned model and provider with usage
// accounting on.
func TestRun_EveryLiveStepRunsThroughTheProductionRuntime(t *testing.T) {
	upstream := &fakeUpstream{}
	up := httptest.NewServer(upstream)
	defer up.Close()
	p, url := startProxy(t, up.URL, 1)

	results := runFamilies(t, url, 0, "K01", "W01", "T01", "C01")
	require.Len(t, results, 4)
	for _, res := range results {
		assert.Empty(t, res.Error, res.Episode)
		assert.Empty(t, res.SeedErrors, res.Episode)
		require.Len(t, res.Turns, 4, "%s: two first-instance turns and two probes", res.Episode)
		for _, turn := range res.Turns[2:] {
			assert.False(t, turn.CorrectionInWindow, "%s step %d: the correction has left the window", res.Episode, turn.Step)
		}
		assert.Equal(t, "- one\n- two\n- three", res.Turns[3].Reply)
	}

	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.NotEmpty(t, upstream.requests)
	for _, req := range upstream.requests {
		assert.JSONEq(t, `"pinned/model"`, string(req["model"]))
		assert.JSONEq(t, `{"only":["Pinned"],"allow_fallbacks":false}`, string(req["provider"]))
		assert.JSONEq(t, `{"include":true}`, string(req["usage"]))
	}
	spent, _ := p.ledger.state()
	assert.InDelta(t, 0.0001*float64(len(upstream.requests)), spent, 1e-9, "the ledger charges the cost the response reports")
}

// TestRun_TheWindowControlSeesACorrectionTheWindowHolds is the near miss
// of the window control: the same family with a window of 256 messages
// restores the correction at the probe, and the control reports it.
func TestRun_TheWindowControlSeesACorrectionTheWindowHolds(t *testing.T) {
	up := httptest.NewServer(&fakeUpstream{})
	defer up.Close()
	_, url := startProxy(t, up.URL, 1)

	results := runFamilies(t, url, 256, "W01")
	require.Len(t, results, 1)
	require.Len(t, results[0].Turns, 4)
	assert.True(t, results[0].Turns[2].CorrectionInWindow)
}

// TestRun_TheRecallControlReportsWhatTheModelWasShown sets the recall
// flag exactly when an act request carried the shipped recall heading.
func TestRun_TheRecallControlReportsWhatTheModelWasShown(t *testing.T) {
	upstream := &fakeUpstream{}
	up := httptest.NewServer(upstream)
	defer up.Close()
	_, url := startProxy(t, up.URL, 1)

	results := runFamilies(t, url, 0, "W01")
	require.Len(t, results, 1)
	shown := false
	upstream.mu.Lock()
	for _, req := range upstream.requests {
		if requestKind(mustJSON(t, req)) == "act" && strings.Contains(string(req["messages"]), recallHeading) {
			shown = true
		}
	}
	upstream.mu.Unlock()
	t.Logf("an act request carried the recall heading: %v", shown)
	for _, turn := range results[0].Turns {
		assert.Equal(t, shown, turn.RecallBlock, "step %d", turn.Step)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// TestRun_AnInfrastructureFailureReRunsTheFamily records a family whose
// upstream is down as failed on infrastructure, moves that record aside on
// the next run, and runs the family again.
func TestRun_AnInfrastructureFailureReRunsTheFamily(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"busy"}}`, http.StatusTooManyRequests)
	}))
	defer down.Close()
	_, url := startProxy(t, down.URL, 1)
	r, err := newRunner(runnerConfig{arm: "X", corpusPath: filepath.Join(repoRoot, "research/general-agent/corpus.json"),
		proxy: url, model: "m", toolsDir: filepath.Join(repoRoot, "tools"), embedder: wordEmbedder{}})
	require.NoError(t, err)
	backoffDisabled(t)
	out := t.TempDir()
	require.NoError(t, r.runAll(out, map[string]bool{"W01": true}, 1))
	first := readResults(t, filepath.Join(out, "results.jsonl"))
	require.Len(t, first, 1)
	assert.True(t, first[0].Infra, first[0].Error)

	up := httptest.NewServer(&fakeUpstream{})
	defer up.Close()
	_, url = startProxy(t, up.URL, 1)
	r.cfg.proxy = url
	require.NoError(t, r.runAll(out, map[string]bool{"W01": true}, 1))
	second := readResults(t, filepath.Join(out, "results.jsonl"))
	require.Len(t, second, 1, "the failed record left results.jsonl")
	assert.False(t, second[0].Infra)
	attempts, err := filepath.Glob(filepath.Join(out, "results-attempt-*.jsonl"))
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Len(t, readResults(t, attempts[0]), 1)
}

// backoffDisabled makes the proxy give up at once on a retryable status.
func backoffDisabled(t *testing.T) {
	t.Helper()
	saved := attemptsLimit
	attemptsLimit = 1
	t.Cleanup(func() { attemptsLimit = saved })
}

func TestProxy_RefusesARequestPastTheCeilingBeforeSendingIt(t *testing.T) {
	upstream := &fakeUpstream{}
	up := httptest.NewServer(upstream)
	defer up.Close()
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],"tools":[]}`

	_, url := startProxy(t, up.URL, 1e-9)
	resp, err := http.Post(url+"/S/K01/live0/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode)
	assert.Empty(t, upstream.requests, "a refused request never reaches the provider")

	// Control: the same request under a ceiling that covers it is sent.
	_, url = startProxy(t, up.URL, 1)
	resp, err = http.Post(url+"/S/K01/live0/v1/chat/completions", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Len(t, upstream.requests, 1)
}

func TestProxy_ACachedSynthesisIsChargedWhatItFirstCost(t *testing.T) {
	upstream := &fakeUpstream{}
	up := httptest.NewServer(upstream)
	defer up.Close()
	p, url := startProxy(t, up.URL, 1)
	body := `{"model":"m","messages":[{"role":"user","content":"batch"}],"tools":[{"type":"function","function":{"name":"memory_summary"}}]}`
	for _, arm := range []string{"S", "H"} {
		resp, err := http.Post(url+"/"+arm+"/K01/seed/v1/chat/completions", "application/json", strings.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}
	assert.Len(t, upstream.requests, 1, "the second arm's identical synthesis is answered from the cache")
	var calls []call
	require.NoError(t, eachLine(filepath.Join(p.cfg.dir, "calls.jsonl"), func(line []byte) error {
		var c call
		calls = append(calls, c)
		return json.Unmarshal(line, &calls[len(calls)-1])
	}))
	require.Len(t, calls, 2)
	assert.Equal(t, 0.0001, calls[0].USD)
	assert.True(t, calls[1].Cached)
	assert.Equal(t, 0.0, calls[1].USD, "the cache answer is not paid again")
	assert.Equal(t, 0.0001, calls[1].CachedUSD, "and the arm it served is charged what it first cost")
}

// TestProxy_BodiesHoldTheExchangeAndNotTheKey records a request and its
// answer in bodies mode, and neither carries the key the proxy sent
// upstream. The control is the upstream itself, which did receive the key.
func TestProxy_BodiesHoldTheExchangeAndNotTheKey(t *testing.T) {
	var gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		(&fakeUpstream{}).ServeHTTP(w, r)
	}))
	defer up.Close()
	p, closeLogs, err := newProxy(proxyConfig{upstream: up.URL, key: "test-key", dir: t.TempDir(), ceiling: 1,
		model: "pinned/model", provider: "Pinned", price: price{input: 1e-8, output: 1e-8}, bodies: true})
	require.NoError(t, err)
	srv := httptest.NewServer(p)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/H/C01/live0/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"write the next part"}]}`))
	require.NoError(t, err)
	resp.Body.Close()
	closeLogs()

	assert.Equal(t, "Bearer test-key", gotAuth)
	data, err := os.ReadFile(filepath.Join(p.cfg.dir, "bodies.jsonl"))
	require.NoError(t, err)
	var body exchangeBody
	require.NoError(t, json.Unmarshal(data, &body))
	assert.Equal(t, "C01", body.Episode)
	assert.Contains(t, string(body.Request), "write the next part")
	assert.Contains(t, string(body.Response), "memory_write")
	assert.NotContains(t, string(data), "test-key")
}

func TestIsInfra(t *testing.T) {
	assert.True(t, isInfra(&openai.APIError{StatusCode: 429}))
	assert.True(t, isInfra(&openai.APIError{StatusCode: 502}))
	assert.True(t, isInfra(&openai.APIError{StatusCode: 402}))
	assert.True(t, isInfra(fmt.Errorf("synthesize conversation: memory summary model: openai provider: HTTP 503: down")))
	// Near misses: a refused request and a model that ignores its tool
	// say something about the run, not the infrastructure.
	assert.False(t, isInfra(&openai.APIError{StatusCode: 400}))
	assert.False(t, isInfra(errors.New("synthesize conversation: model must call memory_summary exactly once")))
}

func TestWorkspace_ListsImmediateChildren(t *testing.T) {
	ws := newWorkspace(nil, map[string]string{"job1/logs/a.log": "1", "job1/logs/b.log": "2", "job1/todo.txt": "x"})
	call := func(name, input string) (string, error) {
		out, err := ws.Call(context.Background(), name, json.RawMessage(input))
		return string(out), err
	}
	out, err := call("file_list", `{"path":"."}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"entries":[{"name":"job1","dir":true}]}`, out)
	out, err = call("file_list", `{"path":"/job1/"}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"entries":[{"name":"logs","dir":true},{"name":"todo.txt","dir":false}]}`, out)
	_, err = call("file_list", `{"path":"job2"}`)
	assert.ErrorContains(t, err, "no such directory")
	_, err = call("file_write", `{"path":"job1/logs","content":"x"}`)
	assert.ErrorContains(t, err, "is a directory")
	_, err = call("file_write", `{"path":"./out/total.txt","content":"3"}`)
	require.NoError(t, err)
	out, err = call("file_read", `{"path":"out/total.txt"}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"3"}`, out)
	_, err = call("file_read", `{"path":"out/missing.txt"}`)
	assert.ErrorContains(t, err, "no such file")
}

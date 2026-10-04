package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureSpend is the model traffic the meter counted.
type fixtureSpend struct {
	Calls            int            `json:"calls"`
	PromptTokens     int64          `json:"prompt_tokens"`
	CompletionTokens int64          `json:"completion_tokens"`
	Statuses         map[string]int `json:"statuses"`
	Refused          int            `json:"refused"`
	Paused           string         `json:"paused,omitempty"`
}

// fixtureMeterConfig is what the meter runs on.
type fixtureMeterConfig struct {
	// upstream is the model endpoint the meter forwards to.
	upstream string
	// listen is the exact <ip>:<port> the meter binds; an unspecified
	// address is refused. Connections are accepted only from that IP and
	// from loopback.
	listen string
	// containerURL is the meter as the agent container reaches it; its
	// path is the prefix the meter strips before forwarding.
	containerURL string
	maxCalls     int
	// log receives one line per event: the method, path, status, latency
	// and usage of a call, or the source address of a refused connection.
	log func(string)
}

// fixtureMeter stands between the agent container and the model endpoint,
// and runs only as long as the test that started it.
//
// The key transits it and is never written: it forwards the agent's own
// Authorization header and keeps no copy, and no line it logs and no body
// it answers with is built from a request header. It counts each answered
// call's tokens from the response's usage, and pauses — refusing every
// later call without forwarding it — on a 429 carrying MiniMax's 2056
// (the subscription's usage limit), or past maxCalls.
type fixtureMeter struct {
	upstream     *url.URL
	prefix       string
	allowed      net.IP
	maxCalls     int
	containerURL string
	addr         string
	logf         func(string)
	client       *http.Client

	mu    sync.Mutex
	count fixtureSpend
}

func startFixtureMeter(t *testing.T, cfg fixtureMeterConfig) *fixtureMeter {
	t.Helper()
	target, err := url.Parse(cfg.upstream)
	require.NoError(t, err)
	reached, err := url.Parse(cfg.containerURL)
	require.NoError(t, err)
	host, _, err := net.SplitHostPort(cfg.listen)
	require.NoError(t, err)
	allowed := net.ParseIP(host)
	require.NotNil(t, allowed, "the meter's listen address %q names an IP address", cfg.listen)
	require.False(t, allowed.IsUnspecified(), "the meter binds one address, never %s", host)
	m := &fixtureMeter{
		upstream: target, prefix: strings.TrimSuffix(reached.Path, "/"),
		allowed: allowed, maxCalls: cfg.maxCalls, containerURL: cfg.containerURL, logf: cfg.log,
		client: &http.Client{Timeout: 10 * time.Minute},
		count:  fixtureSpend{Statuses: map[string]int{}},
	}
	ln, err := net.Listen("tcp", cfg.listen)
	require.NoError(t, err)
	m.addr = ln.Addr().String()
	server := &http.Server{
		Handler:           http.HandlerFunc(m.serve),
		ReadHeaderTimeout: 10 * time.Second,
		// The server's own error log prints what it chooses to; the meter
		// writes only through log.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	return m
}

// fixtureMaxCalls is the call ceiling: SHERLOCK_FIXTURE_MAX_CALLS, or 150.
func fixtureMaxCalls(t *testing.T) int {
	t.Helper()
	v := os.Getenv("SHERLOCK_FIXTURE_MAX_CALLS")
	if v == "" {
		return 150
	}
	n, err := strconv.Atoi(v)
	require.NoError(t, err)
	return n
}

// fixtureMeterLog logs to the test and, where SHERLOCK_FIXTURE_METER_LOG
// names a file, appends there too.
func fixtureMeterLog(t *testing.T) func(string) {
	t.Helper()
	path := os.Getenv("SHERLOCK_FIXTURE_METER_LOG")
	var mu sync.Mutex
	return func(line string) {
		t.Log(line)
		if path == "" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = fmt.Fprintln(f, time.Now().UTC().Format(time.RFC3339Nano)+" "+line)
	}
}

func (m *fixtureMeter) spend() fixtureSpend {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.count
	out.Statuses = map[string]int{}
	for k, v := range m.count.Statuses {
		out.Statuses[k] = v
	}
	return out
}

// refuse reports why the next call is not forwarded, or empty to
// forward it.
func (m *fixtureMeter) refuse() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.count.Paused != "":
		m.count.Refused++
		return "paused: " + m.count.Paused
	case m.count.Calls >= m.maxCalls:
		m.count.Refused++
		return fmt.Sprintf("call ceiling reached: %d", m.maxCalls)
	}
	m.count.Calls++
	return ""
}

func (m *fixtureMeter) serve(w http.ResponseWriter, r *http.Request) {
	// method and path are the only request values any line carries.
	call := r.Method + " " + r.URL.Path
	defer func() {
		if recover() != nil {
			m.logf("meter: " + call + " panicked; recovered without its values")
			http.Error(w, "meter failure", http.StatusInternalServerError)
		}
	}()
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(remote); ip == nil || (!ip.Equal(m.allowed) && !ip.IsLoopback()) {
		m.logf("meter: refused source " + remote + " for " + call)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if reason := m.refuse(); reason != "" {
		m.logf("meter: " + call + " refused: " + reason)
		http.Error(w, reason, http.StatusServiceUnavailable)
		return
	}
	started := time.Now()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		m.logf("meter: " + call + " request body unreadable")
		http.Error(w, "request body unreadable", http.StatusBadRequest)
		return
	}
	target := *m.upstream
	target.Path = strings.TrimSuffix(m.upstream.Path, "/") + strings.TrimPrefix(r.URL.Path, m.prefix)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		m.logf("meter: " + call + " upstream request not built")
		http.Error(w, "upstream request not built", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	resp, err := m.client.Do(req)
	if err != nil {
		// The client's error names the URL and the cause; the meter keeps
		// to its own words so no layer's message is passed on.
		m.tally("transport-error", nil)
		m.logf(fmt.Sprintf("meter: %s -> transport error after %dms", call, time.Since(started).Milliseconds()))
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	prompt, completion := m.tally(strconv.Itoa(resp.StatusCode), answer)
	m.logf(fmt.Sprintf("meter: %s -> %d in %dms prompt_tokens=%d completion_tokens=%d",
		call, resp.StatusCode, time.Since(started).Milliseconds(), prompt, completion))
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(answer)
}

// tally charges one answered call to the run and returns its usage.
func (m *fixtureMeter) tally(status string, answer []byte) (prompt, completion int64) {
	var decoded struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
		BaseResp struct {
			StatusCode int `json:"status_code"`
		} `json:"base_resp"`
	}
	_ = json.Unmarshal(answer, &decoded)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count.Statuses[status]++
	m.count.PromptTokens += decoded.Usage.PromptTokens
	m.count.CompletionTokens += decoded.Usage.CompletionTokens
	if status == "429" && (decoded.BaseResp.StatusCode == 2056 || bytes.Contains(answer, []byte("2056"))) {
		m.count.Paused = "HTTP 429 with code 2056: the subscription's usage limit; no credit is bought and no call is retried"
	}
	return decoded.Usage.PromptTokens, decoded.Usage.CompletionTokens
}

// TestFixtureMeter_ForwardsTheKeyAndWritesItNowhere drives the meter
// through every path that answers or logs — a counted call, the 429/2056
// pause, a call refused while paused, the call ceiling, and an
// unreachable upstream — with a sentinel key. The sentinel reaches the
// upstream on each forwarded call, which is the control: the test sends a
// key the meter really carries. It appears in no log line and no body the
// meter answers with.
func TestFixtureMeter_ForwardsTheKeyAndWritesItNowhere(t *testing.T) {
	const sentinel = "sentinel-key-6f1d2c9b4a7e"

	var mu sync.Mutex
	var received []string
	var output bytes.Buffer
	logf := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		output.WriteString(line + "\n")
	}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("Authorization"))
		calls++
		n := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30}}`)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":2056,"status_msg":"usage limit exceeded"}}`)
	}))
	t.Cleanup(upstream.Close)

	post := func(m *fixtureMeter) (int, string) {
		req, err := http.NewRequest(http.MethodPost, "http://"+m.addr+"/v1/chat/completions", strings.NewReader(`{"model":"MiniMax-M2"}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+sentinel)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		mu.Lock()
		output.Write(body)
		mu.Unlock()
		return resp.StatusCode, string(body)
	}

	paused := startFixtureMeter(t, fixtureMeterConfig{
		upstream: upstream.URL + "/v1", listen: "127.0.0.1:0",
		containerURL: "http://meter.invalid/v1", maxCalls: 10, log: logf,
	})
	code, _ := post(paused)
	assert.Equal(t, http.StatusOK, code)
	code, _ = post(paused)
	assert.Equal(t, http.StatusTooManyRequests, code)
	code, body := post(paused)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, body, "2056")
	spend := paused.spend()
	assert.Equal(t, 2, spend.Calls)
	assert.Equal(t, int64(120), spend.PromptTokens)
	assert.Equal(t, int64(30), spend.CompletionTokens)
	assert.Equal(t, 1, spend.Refused)
	assert.NotEmpty(t, spend.Paused)

	capped := startFixtureMeter(t, fixtureMeterConfig{
		upstream: upstream.URL + "/v1", listen: "127.0.0.1:0",
		containerURL: "http://meter.invalid/v1", maxCalls: 0, log: logf,
	})
	code, body = post(capped)
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, body, "call ceiling reached")

	unreachable := startFixtureMeter(t, fixtureMeterConfig{
		upstream: "http://" + closedLoopbackAddr(t) + "/v1", listen: "127.0.0.1:0",
		containerURL: "http://meter.invalid/v1", maxCalls: 10, log: logf,
	})
	code, _ = post(unreachable)
	assert.Equal(t, http.StatusBadGateway, code)

	mu.Lock()
	defer mu.Unlock()
	// The control: the upstream received the sentinel on both calls the
	// paused meter forwarded, and on no call it refused.
	assert.Equal(t, []string{"Bearer " + sentinel, "Bearer " + sentinel}, received)
	assert.Contains(t, output.String(), "POST /v1/chat/completions -> 200", "the meter logged the counted call")
	assert.Contains(t, output.String(), "transport error", "the meter logged the unreachable upstream")
	assert.NotContains(t, output.String(), sentinel, "the key appears in nothing the meter writes")
}

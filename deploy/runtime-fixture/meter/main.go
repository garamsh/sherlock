// Command meter is the runtime fixture's model meter (#937). It runs as a
// service of the fixture's compose project, between the agent container
// and MiniMax, and counts each answered call's tokens from the response's
// usage.
//
// The upstream is fixed in code and only POST /v1/chat/completions is
// forwarded, so no configuration and no request chooses where a call goes.
// Redirects are refused, never followed. Only the agent container's
// address on the project network is served. The agent's Authorization
// header is forwarded and never written: no log line and no body the meter
// builds carries a request header or a request or response body. Raw
// upstream responses are forwarded to the agent unmodified.
//
// The meter pauses on HTTP 429 with MiniMax code 2056 (the subscription's
// usage limit) and past maxCalls: every later call is refused without
// being forwarded. The counts are served on a second listener, which is
// the only port the project publishes.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	// upstreamBase is the only endpoint the meter forwards to.
	upstreamBase = "https://api.minimax.io"
	// chatPath is the only call the meter forwards.
	chatPath = "/v1/chat/completions"
	// proxyAddr is the project-network listener the agent calls.
	proxyAddr = ":18939"
	// countsAddr is the listener the project publishes on the host.
	countsAddr = ":18940"
	// agentHost is the agent service's name on the project network.
	agentHost = "agent"
	maxCalls  = 150
)

// errRedirect is what the client returns instead of following a redirect.
var errRedirect = errors.New("redirect refused")

// spend is the model traffic the meter counted.
type spend struct {
	Calls            int            `json:"calls"`
	PromptTokens     int64          `json:"prompt_tokens"`
	CompletionTokens int64          `json:"completion_tokens"`
	Statuses         map[string]int `json:"statuses"`
	Refused          int            `json:"refused"`
	Paused           string         `json:"paused,omitempty"`
}

// meter forwards chat-completion calls from the allowed source to upstream
// and counts them.
type meter struct {
	upstream *url.URL
	// allowed returns the addresses a call may come from, read per call
	// because the agent's address changes when its container is recreated.
	allowed  func() []net.IP
	maxCalls int
	logf     func(string)
	client   *http.Client

	mu    sync.Mutex
	count spend
}

func newMeter(upstream *url.URL, allowed func() []net.IP, max int, logf func(string)) *meter {
	return &meter{
		upstream: upstream, allowed: allowed, maxCalls: max, logf: logf,
		client: &http.Client{
			Timeout: 10 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errRedirect
			},
		},
		count: spend{Statuses: map[string]int{}},
	}
}

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC|log.Lmicroseconds)
	upstream, err := url.Parse(upstreamBase)
	if err != nil {
		logger.Fatal("meter: upstream does not parse")
	}
	m := newMeter(upstream, func() []net.IP {
		ips, _ := net.LookupIP(agentHost)
		return ips
	}, maxCalls, func(line string) { logger.Println(line) })

	quiet := log.New(io.Discard, "", 0)
	counts := &http.Server{Addr: countsAddr, Handler: http.HandlerFunc(m.serveCounts),
		ReadHeaderTimeout: 10 * time.Second, ErrorLog: quiet}
	proxy := &http.Server{Addr: proxyAddr, Handler: http.HandlerFunc(m.serveProxy),
		ReadHeaderTimeout: 10 * time.Second, ErrorLog: quiet}
	go func() {
		if err := counts.ListenAndServe(); err != nil {
			logger.Fatal("meter: counts listener stopped")
		}
	}()
	logger.Println("meter: forwarding " + chatPath + " to " + upstreamBase + " for " + agentHost)
	if err := proxy.ListenAndServe(); err != nil {
		logger.Fatal("meter: proxy listener stopped")
	}
}

func (m *meter) snapshot() spend {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.count
	out.Statuses = map[string]int{}
	for k, v := range m.count.Statuses {
		out.Statuses[k] = v
	}
	return out
}

// serveCounts answers GET /spend with the counts.
func (m *meter) serveCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/spend" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.snapshot())
}

// refuse reports why the next call is not forwarded, or empty to forward
// it and count it.
func (m *meter) refuse() string {
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

func (m *meter) allows(remote string) bool {
	ip := net.ParseIP(remote)
	if ip == nil {
		return false
	}
	for _, allowed := range m.allowed() {
		if ip.Equal(allowed) {
			return true
		}
	}
	return false
}

func (m *meter) serveProxy(w http.ResponseWriter, r *http.Request) {
	// The method and path are the only request values any line carries.
	call := r.Method + " " + r.URL.Path
	defer func() {
		if recover() != nil {
			m.logf("meter: " + call + " panicked; recovered without its values")
			http.Error(w, "meter failure", http.StatusInternalServerError)
		}
	}()
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !m.allows(remote) {
		m.logf("meter: refused source " + remote + " for " + call)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != chatPath {
		m.logf("meter: " + call + " is not forwarded")
		http.NotFound(w, r)
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
	target := m.upstream.JoinPath(chatPath)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		m.logf("meter: " + call + " upstream request not built")
		http.Error(w, "upstream request not built", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	resp, err := m.client.Do(req)
	if err != nil {
		// The client's error names the URL and its cause; the meter writes
		// its own words so that no layer's message is passed on.
		outcome := "transport error"
		if errors.Is(err, errRedirect) {
			outcome = "redirect refused"
		}
		m.tally(outcome, nil)
		m.logf(fmt.Sprintf("meter: %s -> %s after %dms", call, outcome, time.Since(started).Milliseconds()))
		http.Error(w, "upstream "+outcome, http.StatusBadGateway)
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

// tally charges one call to the run and returns its usage.
func (m *meter) tally(status string, answer []byte) (prompt, completion int64) {
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

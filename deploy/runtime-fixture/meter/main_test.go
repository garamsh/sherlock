package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sentinel = "sentinel-key-6f1d2c9b4a7e"

// recorder collects everything the meter writes: its log lines and the
// bodies it answers with.
type recorder struct {
	mu  sync.Mutex
	out bytes.Buffer
}

func (r *recorder) logf(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out.WriteString(line + "\n")
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

// upstreamRecorder is a test upstream that records the Authorization
// header of every request it receives.
type upstreamRecorder struct {
	mu   sync.Mutex
	auth []string
}

func (u *upstreamRecorder) seen() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.auth...)
}

func (u *upstreamRecorder) server(t *testing.T, answer func(n int, w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.auth = append(u.auth, r.Header.Get("Authorization"))
		n := len(u.auth)
		u.mu.Unlock()
		answer(n, w)
	}))
	t.Cleanup(s.Close)
	return s
}

// startMeter serves m's proxy on a loopback listener and returns its URL.
func startMeter(t *testing.T, m *meter) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(m.serveProxy))
	t.Cleanup(s.Close)
	return s.URL
}

func only(ip string) func() []net.IP {
	return func() []net.IP { return []net.IP{net.ParseIP(ip)} }
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// post sends a chat call carrying the sentinel key from localIP and
// records the body the meter answers with.
func post(t *testing.T, rec *recorder, meterURL, localIP string) int {
	t.Helper()
	dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(localIP)}, Timeout: 5 * time.Second}
	client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}
	req, err := http.NewRequest(http.MethodPost, meterURL+chatPath, strings.NewReader(`{"model":"MiniMax-M2"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+sentinel)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	rec.logf(string(body))
	return resp.StatusCode
}

// TestMeter_RefusesRedirects: an upstream that redirects to a second
// server gets the call, and the second server receives no request and so
// no Authorization header. The control is the same meter forwarding to an
// upstream that answers, which shows the call leaves the meter at all.
func TestMeter_RefusesRedirects(t *testing.T) {
	var second upstreamRecorder
	target := second.server(t, func(_ int, w http.ResponseWriter) { _, _ = io.WriteString(w, `{}`) })
	var first upstreamRecorder
	redirecting := first.server(t, func(_ int, w http.ResponseWriter) {
		w.Header().Set("Location", target.URL+chatPath)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	rec := &recorder{}
	m := newMeter(mustURL(t, redirecting.URL), only("127.0.0.1"), 10, rec.logf)

	code := post(t, rec, startMeter(t, m), "127.0.0.1")
	assert.Equal(t, http.StatusBadGateway, code)
	assert.Equal(t, []string{"Bearer " + sentinel}, first.seen(), "the call reached the redirecting upstream")
	assert.Empty(t, second.seen(), "the redirect target received no request and no Authorization header")
	assert.Contains(t, rec.String(), "redirect refused")
	assert.NotContains(t, rec.String(), sentinel)

	// Control: an upstream that answers is forwarded to.
	var plain upstreamRecorder
	answering := plain.server(t, func(_ int, w http.ResponseWriter) { _, _ = io.WriteString(w, `{}`) })
	control := newMeter(mustURL(t, answering.URL), only("127.0.0.1"), 10, rec.logf)
	assert.Equal(t, http.StatusOK, post(t, rec, startMeter(t, control), "127.0.0.1"))
	assert.Equal(t, []string{"Bearer " + sentinel}, plain.seen())
}

// TestMeter_RefusesOtherSources: a call from an address that is not the
// agent's is refused, forwarded nowhere, and its source is logged. The
// control is a call from the allowed address through the same meter.
func TestMeter_RefusesOtherSources(t *testing.T) {
	var up upstreamRecorder
	upstream := up.server(t, func(_ int, w http.ResponseWriter) { _, _ = io.WriteString(w, `{}`) })
	rec := &recorder{}
	m := newMeter(mustURL(t, upstream.URL), only("127.0.0.1"), 10, rec.logf)
	meterURL := startMeter(t, m)

	assert.Equal(t, http.StatusForbidden, post(t, rec, meterURL, "127.0.0.2"))
	assert.Empty(t, up.seen(), "a refused source is forwarded nowhere")
	assert.Contains(t, rec.String(), "refused source 127.0.0.2")

	assert.Equal(t, http.StatusOK, post(t, rec, meterURL, "127.0.0.1"))
	assert.Equal(t, []string{"Bearer " + sentinel}, up.seen(), "the allowed source is forwarded")
	assert.Equal(t, 1, m.snapshot().Calls)
}

// TestMeter_KeyAppearsInNothingTheMeterWrites sends a sentinel key through
// every path that answers or logs: a counted call, the 429/2056 pause, a
// call refused while paused, the call ceiling, an unreachable upstream, a
// refused redirect and a refused source. The upstream receiving the
// sentinel on each forwarded call is the control: the key the test sends
// is one the meter really carries.
func TestMeter_KeyAppearsInNothingTheMeterWrites(t *testing.T) {
	rec := &recorder{}
	var up upstreamRecorder
	upstream := up.server(t, func(n int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":120,"completion_tokens":30}}`)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":2056,"status_msg":"usage limit exceeded"}}`)
	})

	paused := newMeter(mustURL(t, upstream.URL), only("127.0.0.1"), 10, rec.logf)
	pausedURL := startMeter(t, paused)
	assert.Equal(t, http.StatusOK, post(t, rec, pausedURL, "127.0.0.1"))
	assert.Equal(t, http.StatusTooManyRequests, post(t, rec, pausedURL, "127.0.0.1"))
	assert.Equal(t, http.StatusServiceUnavailable, post(t, rec, pausedURL, "127.0.0.1"))
	got := paused.snapshot()
	assert.Equal(t, 2, got.Calls)
	assert.Equal(t, int64(120), got.PromptTokens)
	assert.Equal(t, int64(30), got.CompletionTokens)
	assert.Equal(t, 1, got.Refused)
	assert.NotEmpty(t, got.Paused)
	assert.Equal(t, []string{"Bearer " + sentinel, "Bearer " + sentinel}, up.seen(),
		"both forwarded calls carried the key; the one refused while paused was not forwarded")

	capped := newMeter(mustURL(t, upstream.URL), only("127.0.0.1"), 0, rec.logf)
	assert.Equal(t, http.StatusServiceUnavailable, post(t, rec, startMeter(t, capped), "127.0.0.1"))

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	unreachableURL := "http://" + closed.Addr().String()
	require.NoError(t, closed.Close())
	unreachable := newMeter(mustURL(t, unreachableURL), only("127.0.0.1"), 10, rec.logf)
	assert.Equal(t, http.StatusBadGateway, post(t, rec, startMeter(t, unreachable), "127.0.0.1"))

	var redirectTarget upstreamRecorder
	target := redirectTarget.server(t, func(_ int, w http.ResponseWriter) {})
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+chatPath)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)
	redirected := newMeter(mustURL(t, redirecting.URL), only("127.0.0.1"), 10, rec.logf)
	assert.Equal(t, http.StatusBadGateway, post(t, rec, startMeter(t, redirected), "127.0.0.1"))
	assert.Empty(t, redirectTarget.seen())

	denied := newMeter(mustURL(t, upstream.URL), only("127.0.0.1"), 10, rec.logf)
	assert.Equal(t, http.StatusForbidden, post(t, rec, startMeter(t, denied), "127.0.0.2"))

	out := rec.String()
	for _, want := range []string{"-> 200", "-> 429", "refused: paused", "call ceiling reached", "transport error", "redirect refused", "refused source 127.0.0.2"} {
		assert.Contains(t, out, want, "the path was exercised")
	}
	assert.NotContains(t, out, sentinel, "the key appears in no log line and no body the meter writes")
}

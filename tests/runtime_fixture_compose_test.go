package tests

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRuntimeFixture_Compose runs the acceptance sequence against the
// pinned images under deploy/runtime-fixture/docker-compose.yml, with the
// real model behind a meter that counts its calls and tokens. It spends
// model calls, so it runs only when SHERLOCK_FIXTURE_COMPOSE=1 and skips
// otherwise; deploy/runtime-fixture/README.md names the variables it
// reads.
func TestRuntimeFixture_Compose(t *testing.T) {
	if os.Getenv("SHERLOCK_FIXTURE_COMPOSE") != "1" {
		t.Skip("SHERLOCK_FIXTURE_COMPOSE is not 1: the real-model run is opt-in (deploy/runtime-fixture/README.md)")
	}
	storage := fixtureEnv(t, "SHERLOCK_FIXTURE_STORAGE")
	_, err := os.Stat(storage)
	require.True(t, os.IsNotExist(err), "SHERLOCK_FIXTURE_STORAGE %s exists: the fixture runs on fresh storage only", storage)
	require.NoError(t, os.MkdirAll(storage, 0o755))

	meter := startFixtureMeter(t, fixtureConfigValue(t, "model.base-url"))
	rt := newComposeFixture(t, storage, meter)
	defer func() {
		spend := meter.spend()
		t.Logf("model spend: %d calls, %d prompt tokens, %d completion tokens", spend.Calls, spend.PromptTokens, spend.CompletionTokens)
		if spend.Paused != "" {
			t.Errorf("the run paused: %s", spend.Paused)
		}
	}()
	runFixture(t, rt, filepath.Join(storage, "fixture"))
}

func fixtureEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	require.NotEmpty(t, value, "%s is required for the compose run (deploy/runtime-fixture/README.md)", name)
	return value
}

// composeFixture runs the agent and the workspace from the pinned images
// on the daemon DOCKER_HOST names, under the compose project the file
// declares.
type composeFixture struct {
	file      string
	env       []string
	clients   map[string]*http.Client
	agentDir  string
	workspace string
	meter     *fixtureMeter
}

const (
	composeGateway = "https://127.0.0.1:18937"
	composeTEI     = "http://127.0.0.1:18938"
)

func newComposeFixture(t *testing.T, storage string, meter *fixtureMeter) *composeFixture {
	t.Helper()
	f := &composeFixture{file: filepath.Join(moduleRoot(t), "deploy", "runtime-fixture", "docker-compose.yml"), meter: meter}

	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "deploy", "runtime-fixture", "agent.yaml"))
	require.NoError(t, err)
	config := filepath.Join(storage, "agent.yaml")
	require.NoError(t, os.WriteFile(config, raw, 0o600))

	tlsDir := filepath.Join(storage, "tls")
	require.NoError(t, os.MkdirAll(tlsDir, 0o700))
	ca := newE2ECA(t, "sherlock-937-fixture-ca")
	require.NoError(t, os.WriteFile(filepath.Join(tlsDir, "ca.pem"), ca.pem, 0o600))
	ca.issue(t, tlsDir, "server", "localhost", x509.ExtKeyUsageServerAuth,
		[]string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	f.clients = map[string]*http.Client{}
	for _, sender := range []string{fixtureSenderA, fixtureSenderB} {
		cert, key := ca.issue(t, tlsDir, sender, sender, x509.ExtKeyUsageClientAuth, nil, nil)
		f.clients[sender] = mtlsClient(t, ca.pem, cert, key)
	}
	teiData := filepath.Join(storage, "tei")
	require.NoError(t, os.MkdirAll(teiData, 0o755))

	// The compose CLI's environment: the host's, less any credential it
	// carries, plus the fixture's values. The model key is not among them;
	// it reaches the agent container from FIXTURE_AGENT_ENV alone.
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GH_TOKEN", "GITHUB_TOKEN", "MINIMAX_KEY":
			continue
		}
		f.env = append(f.env, kv)
	}
	f.env = append(f.env,
		"FIXTURE_IMAGE_TAG="+fixtureEnv(t, "SHERLOCK_FIXTURE_IMAGE_TAG"),
		"FIXTURE_AGENT_ENV="+fixtureEnv(t, "SHERLOCK_FIXTURE_AGENT_ENV"),
		"FIXTURE_MODEL_BASE_URL="+meter.containerURL,
		"FIXTURE_CONFIG="+config,
		"FIXTURE_TLS="+tlsDir,
		"FIXTURE_TEI_DATA="+teiData,
		"FIXTURE_AGENT_ID="+fixtureAgentID,
		"FIXTURE_EPOCH="+fixtureEpoch,
	)
	t.Cleanup(func() {
		if t.Failed() {
			out, _ := f.compose(context.Background(), "logs", "--no-color", "agent", "workspace").CombinedOutput()
			t.Logf("compose logs:\n%s", out)
		}
		out, err := f.compose(context.Background(), "down", "--remove-orphans").CombinedOutput()
		if err != nil {
			t.Errorf("compose down: %v\n%s", err, out)
		}
	})

	// TEI loads its model before it serves; the agent's first turn embeds.
	f.agentDir, f.workspace = filepath.Join(storage, "unused"), filepath.Join(storage, "unused")
	f.run(t, "up", "-d", "tei")
	f.await(t, 15*time.Minute, "TEI health", func() bool {
		resp, err := http.Get(composeTEI + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	return f
}

func (f *composeFixture) realModel() bool                   { return true }
func (f *composeFixture) baseURL() string                   { return composeGateway }
func (f *composeFixture) client(sender string) *http.Client { return f.clients[sender] }

func (f *composeFixture) spend() *fixtureSpend {
	s := f.meter.spend()
	return &s
}

// compose builds a compose command against the current storage.
func (f *composeFixture) compose(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", f.file}, args...)...)
	cmd.Env = append(append([]string(nil), f.env...),
		"FIXTURE_AGENT_STORAGE="+f.agentDir,
		"FIXTURE_WORKSPACE_STORAGE="+f.workspace,
	)
	return cmd
}

func (f *composeFixture) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := f.compose(context.Background(), args...).CombinedOutput()
	require.NoError(t, err, "docker compose %v: %s", args, out)
	return string(out)
}

func (f *composeFixture) await(t *testing.T, timeout time.Duration, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("%s not ready within %s", what, timeout)
		}
		time.Sleep(time.Second)
	}
}

func (f *composeFixture) start(t *testing.T, agentDir, workspaceDir string) {
	t.Helper()
	f.agentDir, f.workspace = agentDir, workspaceDir
	f.run(t, "up", "-d", "--force-recreate", "--no-deps", "agent", "workspace")
	f.await(t, 3*time.Minute, "the agent's status route", func() bool {
		resp, err := f.client(fixtureSenderA).Get(fixtureURL(f, "/status"))
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

func (f *composeFixture) stop(t *testing.T) int {
	t.Helper()
	id := strings.TrimSpace(f.run(t, "ps", "-aq", "agent"))
	require.NotEmpty(t, id)
	f.run(t, "stop", "-t", "900", "agent")
	out, err := exec.Command("docker", "inspect", "--format", "{{.State.ExitCode}}", id).CombinedOutput()
	require.NoError(t, err, "%s", out)
	code, err := strconv.Atoi(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	// The workspace lives in the agent's network namespace, which the
	// stop took down with it.
	f.run(t, "stop", "workspace")
	return code
}

func (f *composeFixture) secondWriter(t *testing.T, agentDir string) (int, string) {
	t.Helper()
	require.Equal(t, f.agentDir, agentDir, "the second writer runs on the store the agent serves")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := f.compose(ctx, "run", "--rm", "--no-deps", "-T", "agent",
		"agent", "--config=/etc/sherlock/config.yaml", "--strict-config",
		"--agent-id="+fixtureAgentID, "--assignment-epoch="+fixtureEpoch)
	out, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "a second writer on the same store was still running after 3m:\n%s", out)
	return exitCode(err), string(out)
}

func (f *composeFixture) workspaceFile(t *testing.T, _ string, name string) string {
	t.Helper()
	return f.run(t, "exec", "-T", "workspace", "cat", "/var/lib/sherlock-workspace/"+name)
}

// fixtureSpend is the model traffic the meter counted.
type fixtureSpend struct {
	Calls            int            `json:"calls"`
	PromptTokens     int64          `json:"prompt_tokens"`
	CompletionTokens int64          `json:"completion_tokens"`
	Statuses         map[string]int `json:"statuses"`
	Refused          int            `json:"refused"`
	Paused           string         `json:"paused,omitempty"`
}

// fixtureMeter stands between the agent container and the model endpoint.
// It forwards the agent's own Authorization header and keeps no copy of
// it, so the key stays in the agent container's environment. It counts
// each answered call's tokens, and pauses — refusing every later call
// without forwarding it — on a 429 carrying MiniMax's 2056 (subscription
// usage limit), or past SHERLOCK_FIXTURE_MAX_CALLS.
type fixtureMeter struct {
	upstream     *url.URL
	prefix       string
	allowed      net.IP
	maxCalls     int
	containerURL string
	client       *http.Client

	mu    sync.Mutex
	count fixtureSpend
}

func startFixtureMeter(t *testing.T, upstream string) *fixtureMeter {
	t.Helper()
	target, err := url.Parse(upstream)
	require.NoError(t, err)
	listen := fixtureEnv(t, "SHERLOCK_FIXTURE_METER_LISTEN")
	containerURL := fixtureEnv(t, "SHERLOCK_FIXTURE_METER_URL")
	reached, err := url.Parse(containerURL)
	require.NoError(t, err)
	host, _, err := net.SplitHostPort(listen)
	require.NoError(t, err)
	maxCalls := 150
	if v := os.Getenv("SHERLOCK_FIXTURE_MAX_CALLS"); v != "" {
		maxCalls, err = strconv.Atoi(v)
		require.NoError(t, err)
	}
	m := &fixtureMeter{
		upstream: target, prefix: strings.TrimSuffix(reached.Path, "/"),
		allowed: net.ParseIP(host), maxCalls: maxCalls, containerURL: containerURL,
		client: &http.Client{Timeout: 10 * time.Minute},
		count:  fixtureSpend{Statuses: map[string]int{}},
	}
	require.NotNil(t, m.allowed, "SHERLOCK_FIXTURE_METER_LISTEN names an IP address")
	ln, err := net.Listen("tcp", listen)
	require.NoError(t, err)
	server := &http.Server{Handler: http.HandlerFunc(m.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(ln) }()
	t.Cleanup(func() { _ = server.Close() })
	return m
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
	// Only the host's own address reaches the meter: the rootless
	// daemon's network stack connects from it, and nothing else may use
	// this as a relay.
	remote, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(remote); ip == nil || (!ip.Equal(m.allowed) && !ip.IsLoopback()) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if reason := m.refuse(); reason != "" {
		http.Error(w, reason, http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target := *m.upstream
	target.Path = strings.TrimSuffix(m.upstream.Path, "/") + strings.TrimPrefix(r.URL.Path, m.prefix)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	resp, err := m.client.Do(req)
	if err != nil {
		m.tally("transport-error", nil)
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	m.tally(strconv.Itoa(resp.StatusCode), answer)
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(answer)
}

func (m *fixtureMeter) tally(status string, answer []byte) {
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
}

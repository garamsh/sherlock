package tests

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mtlsAgentID is this scenario's own agent ID.
const mtlsAgentID = "e2e-mtls"

// e2eCA is a throwaway in-process certificate authority (issue #173:
// tests generate certs in-process; no fixtures committed).
type e2eCA struct {
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	pem    []byte
	serial atomic.Int64
}

func newE2ECA(t *testing.T, cn string) *e2eCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &e2eCA{
		cert: cert,
		key:  key,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// issue signs a certificate and writes cert/key PEM files into dir.
func (ca *e2eCA) issue(t *testing.T, dir, name, cn string, usage x509.ExtKeyUsage, dnsNames []string, ips []net.IP) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(ca.serial.Add(1)),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPath = filepath.Join(dir, name+".pem")
	keyPath = filepath.Join(dir, name+"-key.pem")
	require.NoError(t, os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certPath, keyPath
}

// mtlsClient returns an HTTP client trusting caPEM; an empty certPath
// means the client presents no certificate.
func mtlsClient(t *testing.T, caPEM []byte, certPath, keyPath string) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))
	cfg := &tls.Config{RootCAs: pool}
	if certPath != "" {
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		require.NoError(t, err)
		cfg.Certificates = []tls.Certificate{pair}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg}}
}

// startAgentMTLS starts "gagent agent" over mTLS on a free loopback
// port, dialing workspaceAddr as the agent's workspace gRPC endpoint,
// waits until the probe client is served, and returns the process
// handle, the base URL, the exit channel, and the agent log path.
func startAgentMTLS(t *testing.T, binary, root, configPath, workspaceAddr string, probe *http.Client) (*exec.Cmd, string, <-chan error, string) {
	t.Helper()
	listen, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listen.Addr().String()
	require.NoError(t, listen.Close())

	aux := t.TempDir()
	logPath := filepath.Join(aux, "agent.log")
	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { logFile.Close() })

	cmd := exec.Command(binary, "agent",
		"--config", configPath, "--addr", addr,
		"--agent-id", mtlsAgentID,
		"--memory-path", filepath.Join(root, "memory.db"),
		"--workspace-addr", workspaceAddr)
	// The script-tool registry reads ./tools by default; the in-tree
	// location `make build-tools` populates in place, so point the env
	// at the repo root's tools/ dir explicitly. The github manifest
	// also requires GH_TOKEN at startup, even though this scenario
	// never invokes github.
	toolsDir := toolsDirForTest(t)
	cmd.Env = append(os.Environ(),
		"GAGENT_TOOLS_DIR="+toolsDir,
		"GH_TOKEN=fake-test-token",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-exited
		}
	})

	baseURL := "https://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-exited:
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("agent exited before serving: %v\nlogs:\n%s", err, logs)
		default:
		}
		resp, err := probe.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			return cmd, baseURL, exited, logPath
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("agent did not serve within 10s\nlogs:\n%s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// subscribeSSEClient is subscribeSSE over a caller-supplied client.
func subscribeSSEClient(t *testing.T, client *http.Client, url string) chan map[string]any {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	t.Cleanup(func() { resp.Body.Close() })

	out := make(chan map[string]any, 64)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var e map[string]any
			if err := json.Unmarshal([]byte(line), &e); err == nil {
				out <- e
			}
		}
	}()
	return out
}

// TestGatewayMTLS_AuthenticatesCallers runs the built agent with a
// tls: section (ADR 0012): a client without a certificate and a client
// certified by a foreign CA are rejected at the handshake, while a
// client certified by the configured CA gets 202 on POST and the SSE
// stream — with the caller identity in the request log.
func TestGatewayMTLS_AuthenticatesCallers(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()
	dir := t.TempDir()

	ca := newE2ECA(t, "e2e-ca")
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, ca.pem, 0o600))
	serverCert, serverKey := ca.issue(t, dir, "server", "localhost",
		x509.ExtKeyUsageServerAuth, []string{"localhost"}, []net.IP{net.ParseIP("127.0.0.1")})
	clientCert, clientKey := ca.issue(t, dir, "client", "orchestrator",
		x509.ExtKeyUsageClientAuth, nil, nil)
	foreign := newE2ECA(t, "foreign-ca")
	foreignCert, foreignKey := foreign.issue(t, dir, "foreign", "intruder",
		x509.ExtKeyUsageClientAuth, nil, nil)

	configPath := filepath.Join(dir, "gagent.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(
		"tls:\n"+
			"  cert: "+serverCert+"\n"+
			"  key: "+serverKey+"\n"+
			"  client-ca: "+caPath+"\n"), 0o600))

	_, workspaceURL, _ := startWorkspaceBinary(t, binary,
		filepath.Join(t.TempDir(), "workspace"), emptyConfig(t, "workspace.yaml"))
	client := mtlsClient(t, ca.pem, clientCert, clientKey)
	_, baseURL, _, logPath := startAgentMTLS(t, binary, root, configPath, workspaceURL, client)

	_, err := mtlsClient(t, ca.pem, "", "").Get(baseURL + "/")
	require.Error(t, err, "a client without a certificate must be rejected at the handshake")

	_, err = mtlsClient(t, ca.pem, foreignCert, foreignKey).Get(baseURL + "/")
	require.Error(t, err, "a client certified by a foreign CA must be rejected at the handshake")

	events := subscribeSSEClient(t, client, baseURL+"/agents/"+mtlsAgentID+"/events")
	resp, err := client.Post(baseURL+"/agents/"+mtlsAgentID+"/messages",
		"application/json", strings.NewReader(`{"content":"hello"}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	resp.Body.Close()

	got := collectEvents(t, events, 10)
	assert.Equal(t, "turn_started", got[0]["kind"])
	assert.Equal(t, "turn_finished", got[len(got)-1]["kind"])

	require.Eventually(t, func() bool {
		logs, err := os.ReadFile(logPath)
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(logs), "\n") {
			if strings.Contains(line, `"caller_id":"orchestrator"`) {
				return strings.Contains(line, `"trace_id":"trace-`)
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond,
		"the request log must carry the caller identity next to trace_id")
}

// TestAgent_NonLoopbackWithoutTLSFailsStartup runs the built agent with
// a non-loopback --addr and no tls: section: startup must fail with a
// clear error instead of serving an unauthenticated port (ADR 0012).
func TestAgent_NonLoopbackWithoutTLSFailsStartup(t *testing.T) {
	binary := buildGagentBinary(t)
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "gagent.yaml")
	require.NoError(t, os.WriteFile(configPath, nil, 0o600))

	for _, addr := range []string{"0.0.0.0:18080", ":18080"} {
		cmd := exec.Command(binary, "agent",
			"--config", configPath, "--addr", addr,
			"--memory-path", filepath.Join(root, "memory.db"))
		out, err := cmd.CombinedOutput()
		require.Error(t, err, "addr %q without tls: must fail startup", addr)
		assert.Contains(t, string(out), "loopback")
		assert.Contains(t, string(out), "tls:")
	}
}

package gateway_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/gateway"
)

// mtlsCA is a throwaway in-process certificate authority for mTLS tests
// (issue #173: no fixtures committed).
type mtlsCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newMTLSCA(t *testing.T) *mtlsCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
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
	return &mtlsCA{cert: cert, key: key}
}

func (ca *mtlsCA) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	return pool
}

// issue signs a client certificate: the CN, or the first DNS SAN when
// the CN is empty, is the caller identity the request log must carry.
func (ca *mtlsCA) issue(t *testing.T, serial int64, cn string, dnsNames []string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dnsNames,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, err)
	return pair
}

// requestLogLines performs one GET per client certificate against an
// mTLS-requiring test server trusting ca and returns the parsed request
// log lines in order.
func requestLogLines(t *testing.T, ca *mtlsCA, certs ...tls.Certificate) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	q, err := gateway.NewQueue(&fakeFactory{}, "env-1", logger)
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewUnstartedServer(gateway.NewHandler(q, logger))
	srv.TLS = &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  ca.pool(),
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	for _, cert := range certs {
		transport := srv.Client().Transport.(*http.Transport).Clone()
		transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
		client := &http.Client{Transport: transport}
		resp, err := client.Get(srv.URL + "/")
		require.NoError(t, err)
		resp.Body.Close()
	}

	var lines []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(line, &entry))
		lines = append(lines, entry)
	}
	return lines
}

func TestServer_RequestLogCarriesCallerIdentity(t *testing.T) {
	ca := newMTLSCA(t)
	cnCert := ca.issue(t, 2, "orchestrator", nil)
	sanCert := ca.issue(t, 3, "", []string{"orchestrator.internal"})

	lines := requestLogLines(t, ca, cnCert, sanCert)
	require.Len(t, lines, 2)

	assert.Equal(t, "orchestrator", lines[0]["caller_id"],
		"the verified certificate's CN is the caller identity")
	assert.Equal(t, "orchestrator.internal", lines[1]["caller_id"],
		"an empty CN falls back to the first SAN")
	for _, line := range lines {
		assert.NotEmpty(t, line["trace_id"], "caller_id lands next to trace_id as trace context")
	}
}

func TestServer_RequestLogHasNoCallerOverPlainHTTP(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	q, err := gateway.NewQueue(&fakeFactory{}, "env-1", logger)
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewServer(gateway.NewHandler(q, logger))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	resp.Body.Close()

	var entry map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry))
	assert.NotContains(t, entry, "caller_id",
		"plain HTTP carries no caller identity")
}

// TestServer_PostMessageSenderIsCallerIdentity pins the sender source
// (ADR 0015): over mTLS the envelope's sender is the verified caller
// identity, and the turn renders it next to the message ID.
func TestServer_PostMessageSenderIsCallerIdentity(t *testing.T) {
	ca := newMTLSCA(t)
	cert := ca.issue(t, 4, "orchestrator", nil)

	session := &fakeSession{}
	q, err := gateway.NewQueue(&fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	}}, "env-1", slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(func() { q.Close() })
	srv := httptest.NewUnstartedServer(gateway.NewHandler(q, slog.New(slog.DiscardHandler)))
	srv.TLS = &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  ca.pool(),
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	transport := srv.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	client := &http.Client{Transport: transport}
	resp, err := client.Post(srv.URL+"/agents/env-1/messages", "application/json",
		strings.NewReader(`{"content":"hello"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Contains(t, session.startedTasks()[0], "] orchestrator: hello",
		"the turn input renders the verified caller identity as the sender")
}

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
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
)

// issueServer signs a console server certificate: 127.0.0.1 is the SAN
// httptest URLs dial, and ServerAuth lets the platform verify the console.
func (ca *mtlsCA) issueServer(t *testing.T, serial int64) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "console"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
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

// warnLog parses buf's JSON log lines and returns the WARN entries.
func warnLog(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(line, &entry))
		if entry["level"] == "WARN" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// outboxMessages returns the messages the fallback outbox captured.
func outboxMessages(t *testing.T, outbox *gateway.Outbox) []message.Message {
	t.Helper()
	entries, err := os.ReadDir(outbox.Dir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var messages []message.Message
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(outbox.Dir(), entry.Name()))
		require.NoError(t, err)
		var msg message.Message
		require.NoError(t, json.Unmarshal(raw, &msg))
		messages = append(messages, msg)
	}
	return messages
}

func TestConsoleSink_DeliversPayloadOverMTLS(t *testing.T) {
	ca := newMTLSCA(t)
	var body []byte
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		body, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		require.NotEmpty(t, r.TLS.PeerCertificates, "the console requires a client certificate")
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{ca.issueServer(t, 10)},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    ca.pool(),
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{ca.issue(t, 11, "platform", nil)},
		RootCAs:      ca.pool(),
	}}}
	outbox := gateway.NewOutbox(t.TempDir())
	sink := gateway.NewConsoleSink("env-1", srv.URL, client, outbox, slog.New(slog.DiscardHandler))

	err := sink.Send(context.Background(), message.Message{
		Channel: "slack", Target: "#general", Content: "hello world",
	})
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"agent_id":"env-1","channel":"slack","target":"#general","content":"hello world"}`,
		string(body))
	assert.Empty(t, outboxMessages(t, outbox), "a delivered message never touches the outbox")
}

func TestConsoleSink_Non2xxFallsBackToOutboxWithWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	outbox := gateway.NewOutbox(t.TempDir())
	sink := gateway.NewConsoleSink("env-1", srv.URL, srv.Client(), outbox, logger)
	msg := message.Message{Channel: "slack", Target: "#general", Content: "hello world"}

	require.NoError(t, sink.Send(context.Background(), msg),
		"the message is preserved by the fallback, so Send reports success")

	assert.Equal(t, []message.Message{msg}, outboxMessages(t, outbox))
	warnings := warnLog(t, &buf)
	require.Len(t, warnings, 1)
	assert.Equal(t, "env-1", warnings[0]["agent_id"])
	assert.Contains(t, warnings[0]["error"], "503")
}

func TestConsoleSink_UnreachableFallsBackToOutboxWithWarn(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	url := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	outbox := gateway.NewOutbox(t.TempDir())
	sink := gateway.NewConsoleSink("env-1", url, &http.Client{}, outbox, logger)
	msg := message.Message{Channel: "slack", Target: "#general", Content: "hello world"}

	require.NoError(t, sink.Send(context.Background(), msg))

	assert.Equal(t, []message.Message{msg}, outboxMessages(t, outbox))
	assert.Len(t, warnLog(t, &buf), 1)
}

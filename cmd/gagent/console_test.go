package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/config"
	"github.com/garamsh/gagent/internal/gateway"
)

// writeSelfSignedCert writes a throwaway self-signed certificate/key
// pair into dir and returns their paths. internal/config's TLS tests
// carry the same scaffolding: Go test helpers do not cross a package
// boundary, and the console sink is the last consumer of TLS material
// left outside that package.
func writeSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "self-signed"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certPath, keyPath
}

func TestConsoleSinkFor_UnsetSection(t *testing.T) {
	newSink, err := consoleSinkFor(config.Config{}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Nil(t, newSink, "no console: section keeps the file outbox")
}

func TestConsoleSinkFor_UnsetTLSFallsBackWithWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg := config.Config{Console: config.ConsoleConfig{URL: "https://console.internal:9443/messages"}}

	newSink, err := consoleSinkFor(cfg, logger)
	require.NoError(t, err)
	assert.Nil(t, newSink, "console delivery needs the tls: material; the outbox serves")
	assert.Contains(t, buf.String(), "console sink unavailable")
	assert.Contains(t, buf.String(), cfg.Console.URL)
}

func TestConsoleSinkFor_MTLSSectionBuildsConsoleSink(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeSelfSignedCert(t, dir)
	cfg := config.Config{
		TLS:     config.TLSConfig{Cert: certPath, Key: keyPath, ClientCA: certPath},
		Console: config.ConsoleConfig{URL: "https://console.internal:9443/messages"},
	}

	newSink, err := consoleSinkFor(cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.NotNil(t, newSink)

	sink := newSink("env-1", gateway.NewOutbox(filepath.Join(dir, "env-1")))
	assert.IsType(t, &gateway.ConsoleSink{}, sink)
}

func TestConsoleSinkFor_UnreadableMaterialFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pem")
	cfg := config.Config{
		TLS:     config.TLSConfig{Cert: missing, Key: missing, ClientCA: missing},
		Console: config.ConsoleConfig{URL: "https://console.internal:9443/messages"},
	}

	_, err := consoleSinkFor(cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err, "console material that cannot load fails startup")
	assert.Contains(t, err.Error(), "console:")
}

package main

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/gateway"
)

func TestConsoleSinkFor_UnsetSection(t *testing.T) {
	newSink, err := consoleSinkFor(Config{}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Nil(t, newSink, "no console: section keeps the file outbox")
}

func TestConsoleSinkFor_UnsetTLSFallsBackWithWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	cfg := Config{Console: ConsoleConfig{URL: "https://console.internal:9443/messages"}}

	newSink, err := consoleSinkFor(cfg, logger)
	require.NoError(t, err)
	assert.Nil(t, newSink, "console delivery needs the tls: material; the outbox serves")
	assert.Contains(t, buf.String(), "console sink unavailable")
	assert.Contains(t, buf.String(), cfg.Console.URL)
}

func TestConsoleSinkFor_MTLSSectionBuildsConsoleSink(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeSelfSignedCert(t, dir)
	cfg := Config{
		TLS:     TLSConfig{Cert: certPath, Key: keyPath, ClientCA: certPath},
		Console: ConsoleConfig{URL: "https://console.internal:9443/messages"},
	}

	newSink, err := consoleSinkFor(cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.NotNil(t, newSink)

	sink := newSink("env-1", gateway.NewOutbox(filepath.Join(dir, "env-1")))
	assert.IsType(t, &gateway.ConsoleSink{}, sink)
}

func TestConsoleSinkFor_UnreadableMaterialFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.pem")
	cfg := Config{
		TLS:     TLSConfig{Cert: missing, Key: missing, ClientCA: missing},
		Console: ConsoleConfig{URL: "https://console.internal:9443/messages"},
	}

	_, err := consoleSinkFor(cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err, "console material that cannot load fails startup")
	assert.Contains(t, err.Error(), "console:")
}

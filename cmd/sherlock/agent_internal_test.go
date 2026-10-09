package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/config"
)

// TestNewVerifier_AKeysFileMustReadAndAKeysURLNeedNot refuses a start
// whose keys-file cannot be read, and starts with a keys-url that cannot
// be fetched (ADR 0026). The control is a readable keys-file, which starts.
func TestNewVerifier_AKeysFileMustReadAndAKeysURLNeedNot(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	path := filepath.Join(t.TempDir(), "keys.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"keys":[]}`), 0o600))
	cfg := config.Config{Issuers: []config.IssuerConfig{{Issuer: "https://a.example", KeysFile: path}}}
	_, err := newVerifier(context.Background(), cfg, "agent", logger)
	require.NoError(t, err, "control: a readable keys-file starts")

	cfg.Issuers[0].KeysFile = filepath.Join(t.TempDir(), "absent.json")
	_, err = newVerifier(context.Background(), cfg, "agent", logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://a.example")

	cfg.Issuers[0] = config.IssuerConfig{Issuer: "https://a.example", KeysURL: "https://127.0.0.1:1/keys"}
	_, err = newVerifier(context.Background(), cfg, "agent", logger)
	assert.NoError(t, err, "an unreachable keys-url leaves the issuer unavailable, not the start refused")
}

// TestNewVerifier_AKeysCAFileMustRead refuses a start whose keys-ca-file
// holds no certificate, though its keys-url need not be reachable. The
// control is the same keys-url without the bundle, which starts.
func TestNewVerifier_AKeysCAFileMustRead(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	cfg := config.Config{Issuers: []config.IssuerConfig{{Issuer: "https://a.example", KeysURL: "https://127.0.0.1:1/keys"}}}
	_, err := newVerifier(context.Background(), cfg, "agent", logger)
	require.NoError(t, err, "control: the keys-url alone starts")

	bundle := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(bundle, []byte("not a certificate"), 0o600))
	cfg.Issuers[0].KeysCAFile = bundle
	_, err = newVerifier(context.Background(), cfg, "agent", logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://a.example")
	assert.Contains(t, err.Error(), "keys-ca-file")
}

package config_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/config"
)

// writeSelfSignedCert writes a throwaway self-signed certificate/key
// pair into dir and returns their paths.
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

func TestServerTLSConfig_UnsetSection(t *testing.T) {
	cfg, err := config.ServerTLS(config.TLSConfig{})
	require.NoError(t, err)
	assert.Nil(t, cfg, "an empty tls: section means plain HTTP")
}

func TestServerTLSConfig_PartialSectionFails(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.TLSConfig
		missing string
	}{
		{"cert only", config.TLSConfig{Cert: "c.pem"}, "key, client-ca"},
		{"key only", config.TLSConfig{Key: "k.pem"}, "cert, client-ca"},
		{"client CA only", config.TLSConfig{ClientCA: "ca.pem"}, "cert, key"},
		{"client CA missing", config.TLSConfig{Cert: "c.pem", Key: "k.pem"}, "client-ca"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.ServerTLS(tt.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.missing)
		})
	}
}

func TestServerTLSConfig_KeyPermissions(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := writeSelfSignedCert(t, dir)
	cfg := config.TLSConfig{Cert: certPEM, Key: keyPEM, ClientCA: certPEM}

	require.NoError(t, os.Chmod(keyPEM, 0o644))
	_, err := config.ServerTLS(cfg)
	require.Error(t, err, "a group/other-readable server key must fail startup")
	assert.Contains(t, err.Error(), keyPEM)
	assert.Contains(t, err.Error(), "0600")

	require.NoError(t, os.Chmod(keyPEM, 0o600))
	_, err = config.ServerTLS(cfg)
	assert.NoError(t, err, "a 0600 server key passes")

	require.NoError(t, os.Chmod(keyPEM, 0o400))
	_, err = config.ServerTLS(cfg)
	assert.NoError(t, err, "a 0400 server key passes: the check is owner-only, not exactly 0600")
}

func TestServerTLSConfig_UnreadableFilesFail(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.pem")

	_, err := config.ServerTLS(config.TLSConfig{Cert: missing, Key: missing, ClientCA: missing})
	require.Error(t, err, "a nonexistent server certificate must fail startup")

	certPEM, keyPEM := writeSelfSignedCert(t, dir)
	_, err = config.ServerTLS(config.TLSConfig{Cert: certPEM, Key: keyPEM, ClientCA: missing})
	require.Error(t, err, "a nonexistent client CA must fail startup")

	garbage := filepath.Join(dir, "garbage.pem")
	require.NoError(t, os.WriteFile(garbage, []byte("not pem"), 0o600))
	_, err = config.ServerTLS(config.TLSConfig{Cert: certPEM, Key: keyPEM, ClientCA: garbage})
	require.Error(t, err, "a client CA file without PEM certificates must fail startup")
	assert.Contains(t, err.Error(), "client CA")
}

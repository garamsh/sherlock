package main

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

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"ipv4 loopback", "127.0.0.1:8080", true},
		{"ipv4 loopback range", "127.1.2.3:8080", true},
		{"localhost", "localhost:8080", true},
		{"localhost case-insensitive", "LOCALHOST:8080", true},
		{"ipv6 loopback", "[::1]:8080", true},
		{"wildcard empty host", ":8080", false},
		{"wildcard ipv4", "0.0.0.0:8080", false},
		{"wildcard ipv6", "[::]:8080", false},
		{"lan address", "192.168.1.10:8080", false},
		{"hostname", "gateway.example.com:8080", false},
		{"unparseable fails safe", "not-an-address", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLoopbackAddr(tt.addr))
		})
	}
}

func TestServerTLSConfig_UnsetSection(t *testing.T) {
	cfg, err := serverTLSConfig(TLSConfig{})
	require.NoError(t, err)
	assert.Nil(t, cfg, "an empty tls: section means plain HTTP")
}

func TestServerTLSConfig_PartialSectionFails(t *testing.T) {
	tests := []struct {
		name    string
		cfg     TLSConfig
		missing string
	}{
		{"cert only", TLSConfig{Cert: "c.pem"}, "key, client-ca"},
		{"key only", TLSConfig{Key: "k.pem"}, "cert, client-ca"},
		{"client CA only", TLSConfig{ClientCA: "ca.pem"}, "cert, key"},
		{"client CA missing", TLSConfig{Cert: "c.pem", Key: "k.pem"}, "client-ca"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := serverTLSConfig(tt.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.missing)
		})
	}
}

func TestServerTLSConfig_UnreadableFilesFail(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.pem")

	_, err := serverTLSConfig(TLSConfig{Cert: missing, Key: missing, ClientCA: missing})
	require.Error(t, err, "a nonexistent server certificate must fail startup")

	certPEM, keyPEM := writeSelfSignedCert(t, dir)
	_, err = serverTLSConfig(TLSConfig{Cert: certPEM, Key: keyPEM, ClientCA: missing})
	require.Error(t, err, "a nonexistent client CA must fail startup")

	garbage := filepath.Join(dir, "garbage.pem")
	require.NoError(t, os.WriteFile(garbage, []byte("not pem"), 0o600))
	_, err = serverTLSConfig(TLSConfig{Cert: certPEM, Key: keyPEM, ClientCA: garbage})
	require.Error(t, err, "a client CA file without PEM certificates must fail startup")
	assert.Contains(t, err.Error(), "client CA")
}

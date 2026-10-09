package gateway_test

import (
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
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/gateway"
)

// privateRoot is a CA generated for the test, trusted by no system pool.
type privateRoot struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newPrivateRoot(t *testing.T) privateRoot {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test private root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return privateRoot{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// serveKeysUnder starts a TLS server for 127.0.0.1 whose certificate root
// issued, serving keys as a JWK Set.
func serveKeysUnder(t *testing.T, root privateRoot, keys ...signingKey) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "keys server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, root.cert, &key.PublicKey, root.key)
	require.NoError(t, err)
	set := jose.JSONWebKeySet{}
	for _, k := range keys {
		set.Keys = append(set.Keys, k.public())
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(set)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func writeBundle(t *testing.T, path string, root privateRoot) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, root.pem, 0o600))
}

// TestNewURLKeySet_TrustsTheBundleUnderAPrivateRoot fetches a set served
// under a private root when keys-ca-file holds that root, and refuses the
// same fetch against the system roots.
func TestNewURLKeySet_TrustsTheBundleUnderAPrivateRoot(t *testing.T) {
	clock := newTestClock()
	root := newPrivateRoot(t)
	srv := serveKeysUnder(t, root, newSigningKey(t, "k1"))
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	writeBundle(t, bundle, root)

	set, err := gateway.NewURLKeySet(testIssuer, srv.URL+"/keys", bundle, clock.Now, newTestLogger())
	require.NoError(t, err)
	assert.NoError(t, set.Load(context.Background()), "control: the bundle holding the root verifies the fetch")

	withoutBundle, err := gateway.NewURLKeySet(testIssuer, srv.URL+"/keys", "", clock.Now, newTestLogger())
	require.NoError(t, err)
	err = withoutBundle.Load(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate")
}

// TestNewURLKeySet_ReadsTheBundleAgainAtEachFetch follows a root rotated
// in the bundle without a new key set: a fetch under a bundle holding
// another root fails, and the next fetch after the file is rewritten
// succeeds.
func TestNewURLKeySet_ReadsTheBundleAgainAtEachFetch(t *testing.T) {
	clock := newTestClock()
	root := newPrivateRoot(t)
	srv := serveKeysUnder(t, root, newSigningKey(t, "k1"))
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	writeBundle(t, bundle, newPrivateRoot(t))

	set, err := gateway.NewURLKeySet(testIssuer, srv.URL+"/keys", bundle, clock.Now, newTestLogger())
	require.NoError(t, err)
	err = set.Load(context.Background())
	require.Error(t, err, "a bundle holding another root does not verify the server")
	assert.Contains(t, err.Error(), "certificate")

	writeBundle(t, bundle, root)
	assert.NoError(t, set.Load(context.Background()), "the next fetch reads the rewritten bundle")
}

// TestNewURLKeySet_RefusesAnUnusableBundle refuses a keys-ca-file that
// cannot be read or holds no certificate. The control is the same path
// holding a root, which is accepted.
func TestNewURLKeySet_RefusesAnUnusableBundle(t *testing.T) {
	clock := newTestClock()
	bundle := filepath.Join(t.TempDir(), "ca.pem")
	writeBundle(t, bundle, newPrivateRoot(t))
	_, err := gateway.NewURLKeySet(testIssuer, "https://127.0.0.1:1/keys", bundle, clock.Now, newTestLogger())
	require.NoError(t, err, "control: a bundle holding a root is accepted")

	require.NoError(t, os.WriteFile(bundle, []byte("not a certificate"), 0o600))
	_, err = gateway.NewURLKeySet(testIssuer, "https://127.0.0.1:1/keys", bundle, clock.Now, newTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "holds no PEM certificates")

	_, err = gateway.NewURLKeySet(testIssuer, "https://127.0.0.1:1/keys", filepath.Join(t.TempDir(), "absent.pem"), clock.Now, newTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read keys-ca-file")
}

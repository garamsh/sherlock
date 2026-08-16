package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
)

// ServerTLS resolves the gateway's TLS setup (ADR 0012): a nil
// config means plain HTTP, a non-nil config serves HTTPS with client
// certificates required and verified against the client CA. A partial
// tls: section is a startup error naming the missing paths.
func ServerTLS(cfg TLSConfig) (*tls.Config, error) {
	if cfg.Cert == "" && cfg.Key == "" && cfg.ClientCA == "" {
		return nil, nil
	}
	var missing []string
	if cfg.Cert == "" {
		missing = append(missing, "cert")
	}
	if cfg.Key == "" {
		missing = append(missing, "key")
	}
	if cfg.ClientCA == "" {
		missing = append(missing, "client-ca")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("tls: section is incomplete: missing %s", strings.Join(missing, ", "))
	}
	if err := RequireOwnerOnly("tls: server key", cfg.Key); err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("tls: load server certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.ClientCA)
	if err != nil {
		return nil, fmt.Errorf("tls: read client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("tls: client CA %q holds no PEM certificates", cfg.ClientCA)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

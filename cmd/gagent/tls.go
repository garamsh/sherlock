package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
)

// serverTLSConfig resolves the gateway's TLS setup (ADR 0017): a nil
// config means plain HTTP, a non-nil config serves HTTPS with client
// certificates required and verified against the client CA. A partial
// tls: section is a startup error naming the missing paths.
func serverTLSConfig(cfg TLSConfig) (*tls.Config, error) {
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
	if err := checkServerKeyMode(cfg.Key); err != nil {
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

// checkServerKeyMode rejects a server key readable by group or other: the
// key is runtime material in the runtime UID's domain (ADR 0006) and must
// stay owner-only so agent-UID children cannot read it. The platform
// validates and fails instead of chowning an operator-provided file.
func checkServerKeyMode(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("tls: stat server key: %w", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("tls: server key %q is group- or other-accessible (mode %04o); it must be mode 0600 (owner-only)", path, perm)
	}
	return nil
}

// isLoopbackAddr reports whether addr binds loopback only. The wildcard
// form (":8080", "0.0.0.0:8080") binds every interface and is not
// loopback; an unparseable address fails safe as non-loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

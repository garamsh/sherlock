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

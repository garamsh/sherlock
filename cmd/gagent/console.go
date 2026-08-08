package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/message"
)

// defaultConsoleTimeout bounds one console delivery POST.
const defaultConsoleTimeout = 10 * time.Second

// consoleSinkFor resolves the agent's sink constructor from the
// console: config section (docs/architecture/gateway.md): nil when the
// section is unset or unusable — the file outbox stays the default and
// the fallback. The console POST reuses the platform's tls: material:
// the platform certificate is the client identity and the same private
// CA verifies the console. A console: section without tls: disables the
// console sink with a WARN instead of failing startup.
func consoleSinkFor(cfg Config, logger *slog.Logger) (func(agentID string, fallback *gateway.Outbox) message.Sink, error) {
	if cfg.Console.URL == "" {
		return nil, nil
	}
	if cfg.TLS == (TLSConfig{}) {
		logger.Warn("console sink unavailable: the tls: section is unset; using the file outbox",
			"op", "startup", "console_url", cfg.Console.URL)
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
	if err != nil {
		return nil, fmt.Errorf("console: load client certificate: %w", err)
	}
	caPEM, err := os.ReadFile(cfg.TLS.ClientCA)
	if err != nil {
		return nil, fmt.Errorf("console: read CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("console: CA %q holds no PEM certificates", cfg.TLS.ClientCA)
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			MinVersion:   tls.VersionTLS12,
		}},
		Timeout: defaultConsoleTimeout,
	}
	return func(agentID string, fallback *gateway.Outbox) message.Sink {
		return gateway.NewConsoleSink(agentID, cfg.Console.URL, client, fallback, logger)
	}, nil
}

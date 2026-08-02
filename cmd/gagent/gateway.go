package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/gateway"
)

// newGatewayCommand returns the "gagent gateway" subcommand: HTTP message
// ingress, the SSE monitoring stream, and graceful shutdown. Callers
// authenticate with mTLS when the tls: config section is set (ADR 0017);
// without it the gateway serves loopback binds only.
func newGatewayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Serve the HTTP gateway",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolveConfig(cmd)
			if err != nil {
				return err
			}
			ego, err := resolveEgo(cmd)
			if err != nil {
				return err
			}
			return serveGateway(cmd.Context(), cfg, ego)
		},
	}
}

// serveGateway runs the HTTP gateway until an interrupt or SIGTERM.
//
// The factory wires the four built-in tools (memory, files, message_send,
// shell_exec) per ADR 0012; the shell_exec executor runs agent-supplied
// commands as sub-processes dropped to the agent UID.
func serveGateway(ctx context.Context, cfg Config, ego string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tlsConfig, err := serverTLSConfig(cfg.TLS)
	if err != nil {
		return err
	}
	if tlsConfig == nil && !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("addr %q is not a loopback bind: serving beyond localhost requires the tls: config section (ADR 0017)", cfg.Addr)
	}

	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return err
	}
	embedder, err := newEmbedderFunc(cfg.Embedder)
	if err != nil {
		return err
	}
	factory, err := newFactory(cfg, newModel, embedder, ego, nil)
	if err != nil {
		return err
	}
	router := gateway.NewRouter(factory, logger)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gateway.NewHandler(router, logger),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown failed", "op", "shutdown", "error", err)
		}
	}()

	if tlsConfig != nil {
		logger.Info("gateway listening",
			"op", "startup", "addr", cfg.Addr, "data_root", cfg.DataRoot,
			"auth", "mtls")
		if err := srv.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("https server failed", "op", "serve", "error", err)
			return err
		}
	} else {
		logger.Info("gateway listening",
			"op", "startup", "addr", cfg.Addr, "data_root", cfg.DataRoot,
			"warning", "no authentication: loopback binds only")
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "op", "serve", "error", err)
			return err
		}
	}
	if err := router.Close(); err != nil {
		logger.Error("router shutdown failed", "op", "shutdown", "error", err)
		return err
	}
	return nil
}

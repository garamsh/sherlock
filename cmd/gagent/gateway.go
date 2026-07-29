package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/sandbox"
)

// newGatewayCommand returns the "gagent gateway" subcommand: HTTP message
// ingress, the SSE monitoring stream, and graceful shutdown. It has no
// authentication and must only be exposed on trusted networks.
func newGatewayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gateway",
		Short: "Serve the HTTP gateway (trusted networks only)",
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
func serveGateway(ctx context.Context, cfg Config, ego string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	docker, err := sandbox.NewDockerRuntime()
	if err != nil {
		logger.Error("docker runtime unavailable", "op", "startup", "error", err)
		return err
	}
	newModel, err := newModelFunc(cfg.Model)
	if err != nil {
		return err
	}
	router := gateway.NewRouter(newFactory(cfg, docker, newModel, ego, nil), logger)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gateway.NewHandler(router, logger),
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

	logger.Info("gateway listening",
		"op", "startup", "addr", cfg.Addr, "data_root", cfg.DataRoot,
		"warning", "no authentication: trusted networks only")
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "op", "serve", "error", err)
		return err
	}
	if err := router.Close(); err != nil {
		logger.Error("router shutdown failed", "op", "shutdown", "error", err)
		return err
	}
	return nil
}

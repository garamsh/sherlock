// Command gateway serves the agent platform over HTTP: fire-and-forget
// message ingress, per-environment serialized runs, and an SSE monitoring
// stream (docs/architecture/gateway.md). It has no authentication and
// must only be exposed on trusted networks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/gateway"
	"github.com/garamsh/gagent/internal/sandbox"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dataRoot := flag.String("data-root", environment.DefaultRoot, "parent directory of all environment data")
	image := flag.String("image", "alpine:3.21", "environment container image")
	execTimeout := flag.Duration("exec-timeout", 30*time.Second, "timeout for one sandbox_exec command")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	docker, err := sandbox.NewDockerRuntime()
	if err != nil {
		logger.Error("docker runtime unavailable", "op", "startup", "error", err)
		os.Exit(1)
	}
	router := gateway.NewRouter(&gateway.Factory{
		Root:        *dataRoot,
		NewModel:    mockModel,
		Sandbox:     docker,
		Image:       *image,
		ExecTimeout: *execTimeout,
	}, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              *addr,
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
		"op", "startup", "addr", *addr, "data_root", *dataRoot,
		"warning", "no authentication: trusted networks only")
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "op", "serve", "error", err)
		os.Exit(1)
	}
	if err := router.Close(); err != nil {
		logger.Error("router shutdown failed", "op", "shutdown", "error", err)
		os.Exit(1)
	}
}

// mockModel is the MVP placeholder model (ADR 0005): a canned script that
// acknowledges each message through message_send and finishes the turn
// with a writeback block. It exhausts after one message per environment;
// a real model replaces it without interface changes.
func mockModel(_ string) agent.Model {
	return agent.NewScriptedModel(
		agent.ScriptedStep{
			ToolCalls: []agent.ToolCall{{
				Name:  "message_send",
				Input: json.RawMessage(`{"channel":"http","target":"sender","content":"Message received."}`),
			}},
		},
		agent.ScriptedStep{
			Content: "Acknowledged the inbound message.\n" +
				"```memory\n" +
				`{"add":[{"content":"exchanged a message over the http channel","type":"episodic"}]}` +
				"\n```",
		},
	)
}

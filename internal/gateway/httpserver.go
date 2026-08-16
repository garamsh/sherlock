package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// shutdownGrace bounds the in-flight requests a stopped server waits
// for before it gives up on them.
const shutdownGrace = 10 * time.Second

// readHeaderTimeout bounds how long a client may take to send its
// request headers; without it a stalled connection holds a goroutine
// for as long as it likes.
const readHeaderTimeout = 10 * time.Second

// Serve runs the gateway's HTTP listener until ctx is done, then drains
// it and closes the queue. A nil tlsConfig serves plain HTTP; a non-nil
// one serves HTTPS with client certificates required (ADR 0012).
//
// Whether a given address may be served at all is the caller's
// decision, not this function's: the loopback rule reads a resolved
// setting, and the entry point is what holds resolved settings.
func Serve(ctx context.Context, addr string, tlsConfig *tls.Config, queue *Queue, logger *slog.Logger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           NewHandler(queue, logger),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown failed", "op", "shutdown", "error", err)
		}
	}()

	if err := listen(srv, tlsConfig); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "op", "serve", "error", err)
		return err
	}
	if err := queue.Close(); err != nil {
		logger.Error("queue shutdown failed", "op", "shutdown", "error", err)
		return err
	}
	return nil
}

// listen starts the server on the transport its TLS configuration
// selects. The certificate and key are already in srv.TLSConfig, so the
// TLS path passes empty paths rather than reading the files twice.
func listen(srv *http.Server, tlsConfig *tls.Config) error {
	if tlsConfig != nil {
		return srv.ListenAndServeTLS("", "")
	}
	return srv.ListenAndServe()
}

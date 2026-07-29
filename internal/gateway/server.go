package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/environment"
)

// maxMessageBody caps inbound message bodies so a sender cannot exhaust
// memory through the ingress endpoint.
const maxMessageBody = 1 << 20 // 1 MiB

// NewHandler returns the gateway's HTTP adapter (ADR 0009):
//
//	POST /envs/{envID}/messages — fire-and-forget ingress; 202 + message ID
//	GET  /envs/{envID}/events   — SSE monitoring stream of loop events
//
// There is no authentication in this phase: the gateway serves trusted
// networks only and must not be exposed (docs/architecture/gateway.md).
func NewHandler(router *Router, logger *slog.Logger) http.Handler {
	h := &handler{router: router, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /envs/{envID}/messages", h.postMessage)
	mux.HandleFunc("GET /envs/{envID}/events", h.events)
	return h.logRequests(mux)
}

type handler struct {
	router *Router
	logger *slog.Logger
}

// postMessageRequest is the ingress body: the message content only. The
// gateway assigns the ID.
type postMessageRequest struct {
	Content string `json:"content"`
}

// postMessageResponse acknowledges a queued message.
type postMessageResponse struct {
	ID     string `json:"id"`
	Env    string `json:"env"`
	Status string `json:"status"`
}

// problem is the error body shape for the ingress endpoint.
type problem struct {
	Error string `json:"error"`
}

func (h *handler) postMessage(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("envID")
	var req postMessageRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMessageBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid message body: "+err.Error())
		return
	}
	if req.Content == "" {
		writeProblem(w, http.StatusBadRequest, "content is required")
		return
	}
	id, err := h.router.Enqueue(r.Context(), envID, req.Content)
	if err != nil {
		switch {
		case errors.Is(err, environment.ErrInvalidID):
			writeProblem(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrRouterClosed):
			writeProblem(w, http.StatusServiceUnavailable, err.Error())
		default:
			h.logger.Error("enqueue failed", "op", "enqueue", "env_id", envID, "error", err)
			writeProblem(w, http.StatusInternalServerError, "enqueue failed")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(postMessageResponse{ID: id, Env: envID, Status: "queued"})
}

// eventPayload is the wire form of one loop event on the SSE stream.
// Which fields are set depends on Kind (see agent.Event).
type eventPayload struct {
	Env        string              `json:"env"`
	Kind       agent.EventKind     `json:"kind"`
	Turn       int                 `json:"turn"`
	Task       string              `json:"task,omitempty"`
	Ops        []agent.WritebackOp `json:"ops,omitempty"`
	ToolName   string              `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage     `json:"tool_input,omitempty"`
	ToolResult string              `json:"tool_result,omitempty"`
	ToolFailed bool                `json:"tool_failed,omitempty"`
	Response   string              `json:"response,omitempty"`
	Error      string              `json:"error,omitempty"`
}

// events streams the environment's loop events as server-sent events.
// Subscribing works with zero publishers and publishing works with zero
// subscribers; the stream ends when the client disconnects or the router
// closes.
func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("envID")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Subscribe before the first flush: once the client has received the
	// response headers, its subscription is guaranteed active.
	ch, cancel := h.router.Events(envID).Subscribe()
	defer cancel()
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(eventPayload{
				Env:        envID,
				Kind:       e.Kind,
				Turn:       e.Turn,
				Task:       e.Task,
				Ops:        e.Ops,
				ToolName:   e.ToolName,
				ToolInput:  e.ToolInput,
				ToolResult: e.ToolResult,
				ToolFailed: e.ToolFailed,
				Response:   e.Response,
				Error:      e.Error,
			})
			if err != nil {
				h.logger.Error("encode event failed", "op", "events", "env_id", envID, "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeProblem(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Error: msg})
}

// statusWriter records the response status for access logging while
// preserving http.Flusher so SSE keeps working through the middleware.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Flush implements http.Flusher by delegating to the wrapped writer.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logRequests logs one line per request with a locally generated
// trace_id (no tracing backend in this phase; stack-go §5). Message
// content is never logged.
func (h *handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID, err := newID("trace")
		if err != nil {
			traceID = "unknown"
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		h.logger.Info("http request",
			"trace_id", traceID,
			"op", r.Method+" "+r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// newID returns a random identifier with the given prefix
// (e.g. "trace-9f2c..."), used for trace IDs and (in service.go) message
// IDs.
func newID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

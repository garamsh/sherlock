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
)

// maxMessageBody caps inbound message bodies so a sender cannot exhaust
// memory through the ingress endpoint.
const maxMessageBody = 1 << 20 // 1 MiB

// NewHandler returns the gateway's HTTP adapter (ADR 0009):
//
//	POST /agents/{agentID}/messages — fire-and-forget ingress; 202 + message ID
//	GET  /agents/{agentID}/events   — SSE monitoring stream of loop events
//
// One process serves one agent (ADR 0018): a path whose agentID is not
// the queue's gets 404. Callers authenticate at the TLS layer (mTLS,
// ADR 0017); the handler itself is unchanged by it.
func NewHandler(q *Queue, logger *slog.Logger) http.Handler {
	h := &handler{queue: q, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/{agentID}/messages", h.postMessage)
	mux.HandleFunc("GET /agents/{agentID}/events", h.events)
	return h.logRequests(mux)
}

type handler struct {
	queue  *Queue
	logger *slog.Logger
}

// serveAgent reports whether the request addresses this process's agent;
// a mismatched ID gets a 404 problem body.
func (h *handler) serveAgent(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("agentID") == h.queue.AgentID() {
		return true
	}
	writeProblem(w, http.StatusNotFound, "unknown agent "+r.PathValue("agentID"))
	return false
}

// postMessageRequest is the ingress body: the message content only. The
// gateway assigns the ID. The envelope's sender (ADR 0020) is the
// verified mTLS caller identity (ADR 0017); plain-HTTP loopback requests
// carry no sender.
type postMessageRequest struct {
	Content string `json:"content"`
}

// postMessageResponse acknowledges a queued message.
type postMessageResponse struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

// problem is the error body shape for the ingress endpoint.
type problem struct {
	Error string `json:"error"`
}

func (h *handler) postMessage(w http.ResponseWriter, r *http.Request) {
	if !h.serveAgent(w, r) {
		return
	}
	agentID := r.PathValue("agentID")
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
	id, err := h.queue.Enqueue(r.Context(), callerID(r), req.Content)
	if err != nil {
		if errors.Is(err, ErrQueueClosed) {
			writeProblem(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		h.logger.Error("enqueue failed", "op", "enqueue", "agent_id", agentID, "error", err)
		writeProblem(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(postMessageResponse{ID: id, AgentID: agentID, Status: "queued"})
}

// eventPayload is the wire form of one loop event on the SSE stream.
// Which fields are set depends on Kind (see agent.Event). Ops is always
// serialized: on a writeback event, `[]` means the model sent an empty
// memory block while `null` means it sent none (issue #193).
type eventPayload struct {
	AgentID    string              `json:"agent_id"`
	Kind       agent.EventKind     `json:"kind"`
	Turn       int                 `json:"turn"`
	Task       string              `json:"task,omitempty"`
	Ops        []agent.WritebackOp `json:"ops"`
	ToolName   string              `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage     `json:"tool_input,omitempty"`
	ToolResult string              `json:"tool_result,omitempty"`
	ToolFailed bool                `json:"tool_failed,omitempty"`
	Response   string              `json:"response,omitempty"`
	Error      string              `json:"error,omitempty"`
	MessageIDs []string            `json:"message_ids,omitempty"`
}

// events streams the agent's loop events as server-sent events.
// Subscribing works with zero publishers and publishing works with zero
// subscribers; the stream ends when the client disconnects or the queue
// closes.
func (h *handler) events(w http.ResponseWriter, r *http.Request) {
	if !h.serveAgent(w, r) {
		return
	}
	agentID := r.PathValue("agentID")
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
	ch, cancel := h.queue.Events().Subscribe()
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
				AgentID:    agentID,
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
				MessageIDs: e.MessageIDs,
			})
			if err != nil {
				h.logger.Error("encode event failed", "op", "events", "agent_id", agentID, "error", err)
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
// trace_id (no tracing backend; stack-go §5). Over mTLS the verified
// caller identity joins the line as caller_id (ADR 0017). Message
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
		attrs := []any{
			"trace_id", traceID,
			"op", r.Method + " " + r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if caller := callerID(r); caller != "" {
			attrs = append(attrs, "caller_id", caller)
		}
		h.logger.Info("http request", attrs...)
	})
}

// callerID names the authenticated caller: the verified peer
// certificate's CN, or its first SAN when the CN is empty (ADR 0017).
// Plain HTTP requests have no caller identity and return "".
func callerID(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	cert := r.TLS.PeerCertificates[0]
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName
	}
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0]
	}
	if len(cert.IPAddresses) > 0 {
		return cert.IPAddresses[0].String()
	}
	if len(cert.URIs) > 0 {
		return cert.URIs[0].String()
	}
	return ""
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

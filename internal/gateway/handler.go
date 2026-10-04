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

	"github.com/garamsh/sherlock/internal/agent"
)

// maxMessageBody caps inbound message bodies so a sender cannot exhaust
// memory through the ingress endpoint.
const maxMessageBody = 1 << 20 // 1 MiB

// NewHandler returns the gateway's HTTP adapter (ADR 0007):
//
//	POST /agents/{agentID}/messages      — fire-and-forget ingress; 202 + message ID
//	GET  /agents/{agentID}/messages/{id} — whether the caller's message completed
//	GET  /agents/{agentID}/events        — SSE monitoring stream of loop events
//
// One process serves one agent (ADR 0017): a path whose agentID is not
// the queue's gets 404. Callers authenticate at the TLS layer (mTLS,
// ADR 0012); the handler itself is unchanged by it.
func NewHandler(q *Queue, logger *slog.Logger) http.Handler {
	h := &handler{queue: q, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/{agentID}/messages", h.postMessage)
	mux.HandleFunc("GET /agents/{agentID}/messages/{id}", h.getMessage)
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

// postMessageRequest is the ingress body: the message content and the
// sender's ID for it, kept byte for byte; the gateway assigns one when it
// is absent. The envelope's sender (ADR 0015) is the verified mTLS caller
// identity (ADR 0012), and the ID is unique within it; plain-HTTP
// loopback requests carry the empty sender.
type postMessageRequest struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

// postMessageResponse acknowledges a queued or completed message.
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
	id, status, err := h.queue.Enqueue(r.Context(), callerID(r), req.ID, req.Content)
	if err != nil {
		switch {
		case errors.Is(err, ErrQueueClosed):
			writeProblem(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, ErrIDReused):
			writeProblem(w, http.StatusConflict, "id reused with different content")
		default:
			h.logger.Error("enqueue failed", "op", "enqueue", "agent_id", agentID, "error", err)
			writeProblem(w, http.StatusInternalServerError, "enqueue failed")
		}
		return
	}
	code, wire := http.StatusAccepted, "queued"
	if status == StatusCompleted {
		code, wire = http.StatusOK, "completed"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(postMessageResponse{ID: id, AgentID: agentID, Status: wire})
}

// messageStatusResponse answers a lookup; the caller verifies the ID.
type messageStatusResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// getMessage reports whether the caller's message completed, within the
// caller's own IDs: another sender's ID reads as unknown.
func (h *handler) getMessage(w http.ResponseWriter, r *http.Request) {
	if !h.serveAgent(w, r) {
		return
	}
	id := r.PathValue("id")
	status, err := h.queue.Status(r.Context(), callerID(r), id)
	if err != nil {
		h.logger.Error("message lookup failed", "op", "lookup", "agent_id", r.PathValue("agentID"), "error", err)
		writeProblem(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	wire := "unknown"
	switch status {
	case StatusCompleted:
		wire = "completed"
	case StatusPending:
		wire = "pending"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(messageStatusResponse{ID: id, Status: wire})
}

// eventPayload is the wire form of one loop event on the SSE stream.
// Which fields are set depends on Kind (see agent.Event). Note is set on a
// writeback event that saved one; its absence means the note is unchanged.
type eventPayload struct {
	AgentID    string          `json:"agent_id"`
	Kind       agent.EventKind `json:"kind"`
	Turn       int             `json:"turn"`
	Task       string          `json:"task,omitempty"`
	Note       *string         `json:"note,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolResult string          `json:"tool_result,omitempty"`
	ToolFailed bool            `json:"tool_failed,omitempty"`
	Response   string          `json:"response,omitempty"`
	Error      string          `json:"error,omitempty"`
	// RecalledIDs and RecalledCount carry the recall event's injected
	// memories (issue #325); RecallOutcome says how the read step
	// reached them, so a question memory could not place is
	// distinguishable on the stream from one it had no answer to
	// (ADR 0019). Without it an operator cannot tell whether the read
	// step works, and the running system is the only place that
	// evidence comes from.
	RecalledIDs   []string            `json:"recalled_ids,omitempty"`
	RecalledCount int                 `json:"recalled_count,omitempty"`
	RecallOutcome agent.RecallOutcome `json:"recall_outcome,omitempty"`
	MessageIDs    []string            `json:"message_ids,omitempty"`
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
				AgentID:       agentID,
				Kind:          e.Kind,
				Turn:          e.Turn,
				Task:          e.Task,
				Note:          e.Note,
				ToolName:      e.ToolName,
				ToolInput:     e.ToolInput,
				ToolResult:    e.ToolResult,
				ToolFailed:    e.ToolFailed,
				Response:      e.Response,
				Error:         e.Error,
				RecalledIDs:   e.RecalledIDs,
				RecalledCount: e.RecalledCount,
				RecallOutcome: e.RecallOutcome,
				MessageIDs:    e.MessageIDs,
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
// caller identity joins the line as caller_id (ADR 0012). Message
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
// certificate's CN, or its first SAN when the CN is empty (ADR 0012).
// It is the principal a message ID is scoped to, so it never names the
// certificate itself and survives renewal under the same subject.
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
// (e.g. "trace-9f2c..."), used for trace IDs and (in queue.go) message
// IDs.
func newID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

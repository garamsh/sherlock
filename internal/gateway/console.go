package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/garamsh/gagent/internal/message"
)

// consolePayload is the console sink's wire format (docs/architecture/
// gateway.md): agent_id is the agent's identity (ADR 0016), which the
// console reads as the sending agent's ID; channel and target are the
// routing labels the agent chose. A distinct shape is justified at the
// wire boundary (docs/convention/simplicity.md).
type consolePayload struct {
	AgentID string `json:"agent_id"`
	Channel string `json:"channel"`
	Target  string `json:"target"`
	Content string `json:"content"`
}

// ConsoleStatusError is the console sink's structured error for a non-2xx
// console response; transport failures surface as wrapped errors instead.
type ConsoleStatusError struct {
	// StatusCode is the console's HTTP response status.
	StatusCode int
}

// Error implements error.
func (e *ConsoleStatusError) Error() string {
	return fmt.Sprintf("console responded with status %d", e.StatusCode)
}

// ConsoleSink is the production message.Sink (ADR 0007): it delivers every
// outbound message to the console endpoint with an HTTP POST. Delivery
// failure — transport error or non-2xx — falls back to the file outbox
// with a WARN log, so a message is never silently lost. The mTLS client
// is the caller's concern (the platform reuses its tls: material).
type ConsoleSink struct {
	agentID  string
	url      string
	client   *http.Client
	fallback *Outbox
	logger   *slog.Logger
}

// NewConsoleSink returns a sink POSTing outbound messages to url as
// agentID, falling back to fallback on delivery failure.
func NewConsoleSink(agentID, url string, client *http.Client, fallback *Outbox, logger *slog.Logger) *ConsoleSink {
	return &ConsoleSink{
		agentID:  agentID,
		url:      url,
		client:   client,
		fallback: fallback,
		logger:   logger,
	}
}

// Send implements message.Sink.
func (s *ConsoleSink) Send(ctx context.Context, msg message.Message) error {
	payload, err := json.Marshal(consolePayload{
		AgentID: s.agentID,
		Channel: msg.Channel,
		Target:  msg.Target,
		Content: msg.Content,
	})
	if err != nil {
		return fmt.Errorf("encode console message: %w", err)
	}
	if err := s.post(ctx, payload); err != nil {
		s.logger.Warn("console delivery failed; message lands in the file outbox",
			"op", "message_send",
			"agent_id", s.agentID,
			"channel", msg.Channel,
			"target", msg.Target,
			"error", err)
		return s.fallback.Send(ctx, msg)
	}
	return nil
}

func (s *ConsoleSink) post(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build console request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("post to console: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &ConsoleStatusError{StatusCode: resp.StatusCode}
	}
	return nil
}

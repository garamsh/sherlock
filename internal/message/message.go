package message

import "context"

// Message is one outbound message: which channel, which target, what
// content. The sender, another person, and another agent are all the same
// kind of target (docs/architecture/gateway.md).
type Message struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
	Content string `json:"content"`
}

// Sink is the outbound delivery endpoint for agent-decided messages. The
// gateway wires the real sink; tests use a recording fake.
type Sink interface {
	// Send queues msg for outbound delivery.
	Send(ctx context.Context, msg Message) error
}

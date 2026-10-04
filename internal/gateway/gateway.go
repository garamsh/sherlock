package gateway

import (
	"errors"
	"time"
)

// ErrQueueClosed is returned when a message is enqueued on a closed
// queue.
var ErrQueueClosed = errors.New("gateway: queue closed")

// ErrIDReused is returned when a sender's message ID is already queued,
// running or committed with different content.
var ErrIDReused = errors.New("gateway: id reused with different content")

// Status is where a sender's message stands, as this process can tell.
type Status int

const (
	// StatusUnknown: this process has not seen the message, or lost it
	// when it restarted, or its turn ended without committing it.
	StatusUnknown Status = iota
	// StatusPending: the message is queued or running in this process.
	StatusPending
	// StatusCompleted: the message's turn committed its sources, holding
	// this ID and this content, to memory.
	StatusCompleted
)

// Inbound is one accepted message: it carries its sender's ID or one the
// gateway assigned, and waits for (or is in) the agent's serialized run.
// Sender is the caller's identity; empty means the channel provided none.
// An ID is unique within its sender.
type Inbound struct {
	ID       string
	Sender   string
	Content  string
	QueuedAt time.Time
}

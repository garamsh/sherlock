package gateway

import (
	"errors"
	"time"
)

// ErrDraining is returned when a message is enqueued once the queue has
// begun to drain: the process is shutting down and admits no new work.
var ErrDraining = errors.New("gateway: draining")

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

// Instance is what this process reports about itself on its status
// route, each value taken from what it loaded once startup succeeded. An
// empty AssignmentEpoch, Generation or ConfigRevision means unknown.
type Instance struct {
	// AssignmentEpoch is the launcher's opaque epoch for this activation.
	AssignmentEpoch string
	// Generation is this start's runtime nonce (ADR 0016).
	Generation string
	// ConfigRevision echoes the config file's revision key, never a
	// value computed from the configuration.
	ConfigRevision string
	// StartedAt is the process start time. It is not, by itself,
	// evidence that a revision was applied.
	StartedAt time.Time
}

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

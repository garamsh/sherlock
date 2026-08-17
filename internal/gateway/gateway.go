package gateway

import (
	"errors"
	"time"
)

// ErrQueueClosed is returned when a message is enqueued on a closed
// queue.
var ErrQueueClosed = errors.New("gateway: queue closed")

// Inbound is one accepted message: it has been assigned an ID and waits
// for (or is in) the agent's serialized run. Sender is the caller's
// identity; empty means the channel provided none.
type Inbound struct {
	ID       string
	Sender   string
	Content  string
	QueuedAt time.Time
}

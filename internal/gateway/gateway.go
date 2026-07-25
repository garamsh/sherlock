package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrRouterClosed is returned when a message is enqueued on a closed
// router.
var ErrRouterClosed = errors.New("gateway: router closed")

// Inbound is one accepted message: it has been assigned an ID and waits
// for (or is in) its environment's serialized run.
type Inbound struct {
	ID       string
	EnvID    string
	Content  string
	QueuedAt time.Time
}

// newID returns a random identifier with the given prefix
// (e.g. "msg-9f2c..."), used for message and trace IDs.
func newID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

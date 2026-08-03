package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/garamsh/gagent/internal/message"
)

// OutboxDir is the subdirectory of the environment directory that holds
// delivered outbound messages.
const OutboxDir = "outbox"

// Outbox is the MVP message.Sink (ADR 0009): it "delivers" every outbound
// message by appending a JSON file to data/envs/<agent-id>/outbox/. Real
// channel adapters replace it without touching the tool or the agent.
// Outbox is safe for concurrent use.
type Outbox struct {
	dir string

	mu  sync.Mutex
	seq int
}

// NewOutbox returns a sink writing to <envDir>/outbox/.
func NewOutbox(envDir string) *Outbox {
	return &Outbox{dir: filepath.Join(envDir, OutboxDir)}
}

// Dir returns the directory the outbox writes to.
func (o *Outbox) Dir() string { return o.dir }

// Send implements message.Sink. Filenames are timestamp plus sequence so
// delivery order is lexicographic and unique within the process.
func (o *Outbox) Send(_ context.Context, msg message.Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode outbound message: %w", err)
	}
	o.mu.Lock()
	o.seq++
	name := fmt.Sprintf("%d-%06d.json", time.Now().UnixNano(), o.seq)
	o.mu.Unlock()
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		return fmt.Errorf("create outbox %q: %w", o.dir, err)
	}
	if err := os.WriteFile(filepath.Join(o.dir, name), payload, 0o644); err != nil {
		return fmt.Errorf("write outbound message: %w", err)
	}
	return nil
}

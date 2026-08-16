package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/garamsh/gagent/internal/message"
)

// OutboxDir is the subdirectory next to the memory file that holds
// delivered outbound messages.
const OutboxDir = "outbox"

// Outbox is the MVP message.Sink (ADR 0007): it "delivers" every outbound
// message by appending a JSON file to <memory dir>/outbox/. Real channel
// adapters replace it without touching the tool or the agent.
// Outbox is safe for concurrent use.
type Outbox struct {
	dir string

	mu  sync.Mutex
	seq int
}

// DirFor returns the outbox directory that belongs to a memory file:
// the subdirectory beside it. The message_send tool the platform spawns
// writes there, and a reader of this package finds what it wrote there,
// so the two agree by naming the same function rather than by both
// spelling the same join.
func DirFor(memoryPath string) string {
	return filepath.Join(filepath.Dir(memoryPath), OutboxDir)
}

// NewOutbox returns a sink writing to <dir>/outbox/.
func NewOutbox(dir string) *Outbox {
	return &Outbox{dir: filepath.Join(dir, OutboxDir)}
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

// outboxSnapshot returns the names of the files currently in dir. A
// turn diffs it against the directory afterwards to find the messages
// it delivered; an unreadable or absent directory is an empty snapshot,
// because a turn that delivers nothing is not a failure.
func outboxSnapshot(dir string) map[string]struct{} {
	out := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e.Name()] = struct{}{}
	}
	return out
}

// echoOutbox writes every message delivered since the snapshot to w, in
// name order, which is delivery order: the writer names each file by
// timestamp and sequence. A file that will not read or parse is skipped
// — the echo is a convenience over the durable record, so it never
// fails a turn that already delivered.
func echoOutbox(w io.Writer, dir string, before map[string]struct{}) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if _, seen := before[e.Name()]; !seen {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var msg message.Message
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		fmt.Fprintf(w, "[%s -> %s] %s\n", msg.Channel, msg.Target, msg.Content)
	}
}

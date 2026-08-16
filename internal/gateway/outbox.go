package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/garamsh/gagent/internal/message"
)

// OutboxDir is the subdirectory next to the memory file the
// message_send tool delivers into. Outbound delivery is that tool
// writing a JSON file there (ADR 0007: outbound is a tool); this package
// only names the directory and reads what the tool left.
const OutboxDir = "outbox"

// DirFor returns the outbox directory that belongs to a memory file:
// the subdirectory beside it. The message_send tool the platform spawns
// writes there, and a reader of this package finds what it wrote there,
// so the two agree by naming the same function rather than by both
// spelling the same join.
func DirFor(memoryPath string) string {
	return filepath.Join(filepath.Dir(memoryPath), OutboxDir)
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

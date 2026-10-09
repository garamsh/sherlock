// message_send is the platform's message_send tool. It implements the
// script-tool runner's stdio protocol (ADR 0009 §Decision): one JSON
// object on stdin (the validated input), one JSON object on stdout
// (the validated output), non-zero exit on failure. The tool writes
// the outbound message as one outbox entry in SHERLOCK_OUTBOX_DIR,
// stamped with its creation time, the sending agent's ID from SHERLOCK_AGENT_ID, its
// provenance from SHERLOCK_ASSIGNMENT_EPOCH and SHERLOCK_GENERATION
// (ADR 0016), and the turn's message IDs from SHERLOCK_IN_REPLY_TO; whatever
// consumes the agent's replies forwards the entry (ADR 0007).
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/sherlock/internal/message"
)

type callInput struct {
	Channel string `json:"channel"`
	Target  string `json:"target"`
	Content string `json:"content"`
}

type callOutput struct {
	Status string `json:"status"`
}

func main() {
	if err := run(time.Now); err != nil {
		fmt.Fprintln(os.Stderr, "message_send:", err)
		os.Exit(1)
	}
}

// run writes one entry, minted at the single instant now returns.
func run(now func() time.Time) error {
	outboxDir := os.Getenv("SHERLOCK_OUTBOX_DIR")
	if outboxDir == "" {
		return errors.New("SHERLOCK_OUTBOX_DIR is empty")
	}
	agentID := os.Getenv("SHERLOCK_AGENT_ID")
	if agentID == "" {
		return errors.New("SHERLOCK_AGENT_ID is empty")
	}
	var inReplyTo []string
	if err := json.Unmarshal([]byte(os.Getenv("SHERLOCK_IN_REPLY_TO")), &inReplyTo); err != nil {
		return fmt.Errorf("decode SHERLOCK_IN_REPLY_TO: %w", err)
	}
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	var in callInput
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	mintedAt := now()
	id, err := newEntryID(mintedAt)
	if err != nil {
		return err
	}
	if err := writeEntry(outboxDir, message.Message{
		ID:              id,
		CreatedAt:       mintedAt.UTC(),
		AgentID:         agentID,
		AssignmentEpoch: os.Getenv("SHERLOCK_ASSIGNMENT_EPOCH"),
		Generation:      os.Getenv("SHERLOCK_GENERATION"),
		InReplyTo:       inReplyTo,
		Channel:         in.Channel,
		Target:          in.Target,
		Content:         in.Content,
	}); err != nil {
		return fmt.Errorf("outbox write failed: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(callOutput{Status: "queued"})
}

// newEntryID returns a fresh entry ID: at in nanoseconds, zero-padded so
// name order is write order, and 64 random bits so two writes in the same
// nanosecond still differ. The layout is this tool's; readers treat the
// ID as opaque and read the time from created_at.
func newEntryID(at time.Time) (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("entry id: %w", err)
	}
	return fmt.Sprintf("%020d-%s", at.UnixNano(), hex.EncodeToString(suffix[:])), nil
}

// writeEntry writes msg to dir as <msg.ID>.json, atomically and
// durably: a temporary file in dir, fsynced, renamed onto the entry's
// name, then the directory fsynced. A reader never sees a partial entry,
// because the name appears only once the content is complete. The rename
// refuses an existing name, so an entry once written is never replaced.
//
// The temporary file is dot-prefixed and has no .json suffix, so a
// reader that takes only *.json names never picks it up.
func writeEntry(dir string, msg message.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	// Owner-only: the agent and the tools it spawns run as one uid, and
	// no other account may list what it delivered (#998).
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".entry-*.tmp")
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := renameNoReplace(tmp.Name(), filepath.Join(dir, msg.ID+".json")); err != nil {
		return err
	}
	renamed = true
	return syncDir(dir)
}

// syncDir fsyncs dir so the rename that named an entry survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

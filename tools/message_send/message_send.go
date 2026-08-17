// message_send is the platform's message_send tool. It implements the
// script-tool runner's stdio protocol (ADR 0009 §Decision): one JSON
// object on stdin (the validated input), one JSON object on stdout
// (the validated output), non-zero exit on failure. The tool writes
// the outbound message to the file outbox at GAGENT_OUTBOX_DIR, stamped
// with the sending agent's ID from GAGENT_AGENT_ID (ADR 0016); the
// platform's message_send plumbing delivers the queue.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/garamsh/gagent/internal/message"
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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "message_send:", err)
		os.Exit(1)
	}
}

func run() error {
	outboxDir := os.Getenv("GAGENT_OUTBOX_DIR")
	if outboxDir == "" {
		return errors.New("GAGENT_OUTBOX_DIR is empty")
	}
	agentID := os.Getenv("GAGENT_AGENT_ID")
	if agentID == "" {
		return errors.New("GAGENT_AGENT_ID is empty")
	}
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	var in callInput
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	if err := writeOutbox(outboxDir, message.Message{
		AgentID: agentID,
		Channel: in.Channel,
		Target:  in.Target,
		Content: in.Content,
	}); err != nil {
		return fmt.Errorf("outbox write failed: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(callOutput{Status: "queued"})
}

// writeOutbox writes a single message to the outbox directory. The
// directory is the agent's default file outbox; the agent reads the
// same directory, so the message lands on the same delivery queue
// the in-process sink used.
func writeOutbox(dir string, msg message.Message) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), sanitize(msg.Target))
	path := filepath.Join(dir, name)
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(body); err != nil {
		return err
	}
	return nil
}

// sanitize strips path separators and whitespace from the target so the
// outbox filename is safe.
func sanitize(target string) string {
	out := make([]byte, 0, len(target))
	for i := 0; i < len(target); i++ {
		c := target[i]
		switch c {
		case '/', '\\', ' ', '\n', '\t', '\r':
			out = append(out, '_')
		default:
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

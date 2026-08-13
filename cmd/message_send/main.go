// message_send is the platform's message_send tool binary (ADR 0018
// §Decision). It serves the platform protocol over stdin/stdout:
// tools/list advertises the descriptor (input and output JSON Schema),
// tools/call validates the input against the schema, writes the
// outbound message to the file outbox directory held in
// GAGENT_OUTBOX_DIR, and returns the structured result.
//
// The agent binary never imports this package in its address space;
// it spawns the binary via the operator's tool list (ADR 0009
// §Decision). The outbox path is operator-supplied per the per-tool
// env contract; the platform never ships a secret here.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/garamsh/gagent/internal/message"
)

// descriptorName is the tool name the binary advertises.
const descriptorName = "message_send"

// descriptorDescription is the one-sentence summary the model sees.
const descriptorDescription = "Queue an outbound message for delivery to a channel and target."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "message_send:", err)
		os.Exit(1)
	}
}

func run() error {
	enc := json.NewEncoder(os.Stdout)
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	mu := &sync.Mutex{}
	outboxDir := os.Getenv("GAGENT_OUTBOX_DIR")
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      uint64          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return nil
		}
		switch req.Method {
		case "tools/list":
			mu.Lock()
			handleList(enc, req.ID)
			mu.Unlock()
		case "tools/call":
			mu.Lock()
			handleCall(enc, req.ID, outboxDir, req.Params)
			mu.Unlock()
		default:
			mu.Lock()
			writeError(enc, req.ID, -32601, "method not found", req.Method)
			mu.Unlock()
		}
	}
}

func handleList(enc *json.Encoder, id uint64) {
	raw, _ := json.Marshal(map[string]any{
		"tools": []map[string]any{{
			"name":         descriptorName,
			"description":  descriptorDescription,
			"inputSchema":  json.RawMessage(message.InputSchema()),
			"outputSchema": json.RawMessage(message.OutputSchema()),
		}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
}

func handleCall(enc *json.Encoder, id uint64, outboxDir string, params json.RawMessage) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		writeError(enc, id, -32600, "invalid params", err.Error())
		return
	}
	if p.Name != descriptorName {
		writeError(enc, id, -32602, "unknown tool", p.Name)
		return
	}
	var msg message.Message
	if err := json.Unmarshal(p.Arguments, &msg); err != nil {
		writeError(enc, id, -32602, "invalid arguments", err.Error())
		return
	}
	if outboxDir == "" {
		writeError(enc, id, -32603, "outbox not configured", "GAGENT_OUTBOX_DIR is empty")
		return
	}
	if err := writeOutbox(outboxDir, msg); err != nil {
		writeError(enc, id, -32603, "outbox write failed", err.Error())
		return
	}
	out, _ := json.Marshal(message.CallOutput{Status: "queued"})
	text := string(out)
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
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

func writeError(enc *json.Encoder, id uint64, code int, msg, data string) {
	raw, _ := json.Marshal(map[string]any{
		"code":    code,
		"message": msg,
		"data":    data,
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Error   json.RawMessage `json:"error"`
	}{"2.0", id, raw})
}

var _ = errors.New

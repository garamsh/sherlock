// files is the platform's files tool binary (ADR 0018 §Decision). It
// serves the platform protocol over stdin/stdout: tools/list advertises
// the descriptor (input and output JSON Schema), tools/call routes
// the operation (read, write, list) through the workspace gRPC client
// at GAGENT_WORKSPACE_ADDR, and returns the structured result.
//
// The agent binary never imports this package in its address space;
// it spawns the binary via the operator's tool list (ADR 0009
// §Decision). The workspace address is operator-supplied per the
// per-tool env contract.
package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/gagent/internal/workspace"
)

// schemaFile holds the tool's input and output JSON Schema document.
//
//go:embed schema.json
var schemaFile []byte

// schemaDoc mirrors the on-disk shape of schema.json.
type schemaDoc struct {
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

var schema = func() schemaDoc {
	var doc schemaDoc
	if err := json.Unmarshal(schemaFile, &doc); err != nil {
		panic("files: embedded schema.json is malformed: " + err.Error())
	}
	return doc
}()

// descriptorName is the tool name the binary advertises.
const descriptorName = "files"

// descriptorDescription is the one-sentence summary the model sees.
const descriptorDescription = "Read, write, and list files inside the agent's workspace."

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "files:", err)
		os.Exit(1)
	}
}

func run() error {
	workspaceAddr := os.Getenv("GAGENT_WORKSPACE_ADDR")
	if workspaceAddr == "" {
		return fmt.Errorf("GAGENT_WORKSPACE_ADDR is required")
	}
	conn, err := grpc.NewClient(workspaceAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial workspace: %w", err)
	}
	defer conn.Close()
	ws := workspace.NewWorkspaceClient(conn)

	enc := json.NewEncoder(os.Stdout)
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	mu := &sync.Mutex{}
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
			handleCall(enc, req.ID, ws, req.Params)
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
			"inputSchema":  schema.Input,
			"outputSchema": schema.Output,
		}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
}

// callInput mirrors the schema's input shape.
type callInput struct {
	Operation string `json:"operation"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// callOutput is the per-operation JSON shape the model receives. The
// declared output schema is an unconstrained object, so any of these
// shapes is valid; the schema validator only checks it's an object.
type readOutput struct {
	Content string `json:"content"`
}

type writeOutput struct {
	BytesWritten int `json:"bytes_written"`
}

type entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
}

type listOutput struct {
	Entries []entry `json:"entries"`
}

func handleCall(enc *json.Encoder, id uint64, ws workspace.WorkspaceClient, params json.RawMessage) {
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
	var args callInput
	if err := json.Unmarshal(p.Arguments, &args); err != nil {
		writeError(enc, id, -32602, "invalid arguments", err.Error())
		return
	}
	if args.Path == "" {
		writeError(enc, id, -32602, "invalid arguments", "path is required")
		return
	}
	var body any
	switch args.Operation {
	case "read":
		out, err := ws.Read(context.Background(), &workspace.ReadRequest{Path: args.Path})
		if err != nil {
			writeError(enc, id, -32603, "read failed", err.Error())
			return
		}
		body = readOutput{Content: string(out.Content)}
	case "write":
		if _, err := ws.Write(context.Background(), &workspace.WriteRequest{Path: args.Path, Content: []byte(args.Content)}); err != nil {
			writeError(enc, id, -32603, "write failed", err.Error())
			return
		}
		body = writeOutput{BytesWritten: len(args.Content)}
	case "list":
		out, err := ws.List(context.Background(), &workspace.ListRequest{Path: args.Path})
		if err != nil {
			writeError(enc, id, -32603, "list failed", err.Error())
			return
		}
		entries := make([]entry, 0, len(out.Entries))
		for _, e := range out.Entries {
			entries = append(entries, entry{Name: e.GetName(), Dir: e.GetDir()})
		}
		body = listOutput{Entries: entries}
	default:
		writeError(enc, id, -32602, "invalid arguments", "unknown operation "+args.Operation)
		return
	}
	bodyBytes, _ := json.Marshal(body)
	text := string(bodyBytes)
	raw, _ := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
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

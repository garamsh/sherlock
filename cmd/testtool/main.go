// testtool is a small tool binary that serves the platform protocol for
// the tool client's tests. It reads its descriptor and call behavior
// from a JSON file at the path held in the GAGENT_TESTTOOL_CONFIG env
// var, and runs one tool over stdin/stdout. The shape is the minimum
// the platform protocol requires (JSON-RPC 2.0, tools/list, tools/call)
// and no more — the test helper is not a real tool.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// config is the on-disk shape of the testtool config file: the
// descriptor the server advertises in tools/list, and the behaviour
// the call handler runs. The two behaviours cover the test surface
// the platform protocol client exercises: round-trip the input as the
// output (echo), or substitute a fixed output regardless of input.
type config struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	// Behaviour selects what the call handler returns. "echo" mirrors
	// the input object as the output (the test case for round-tripping).
	// "fixed" takes the OutputSchema-shaped payload from FixedOutput.
	// "reverse" calls Error on the input — used to exercise the
	// error paths.
	Behaviour string          `json:"behaviour"`
	Fixed     json.RawMessage `json:"fixedOutput,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "testtool:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := os.Getenv("GAGENT_TESTTOOL_CONFIG")
	if configPath == "" {
		return errors.New("GAGENT_TESTTOOL_CONFIG is required")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
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
			handleList(enc, req.ID, cfg)
		case "tools/call":
			handleCall(enc, req.ID, cfg, req.Params)
		default:
			writeError(enc, req.ID, -32601, "method not found", req.Method)
		}
	}
}

func handleList(enc *json.Encoder, id uint64, cfg config) {
	raw, _ := json.Marshal(map[string]any{
		"tools": []map[string]any{{
			"name":         cfg.Name,
			"description":  cfg.Description,
			"inputSchema":  cfg.InputSchema,
			"outputSchema": cfg.OutputSchema,
		}},
	})
	_ = enc.Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{"2.0", id, raw})
}

func handleCall(enc *json.Encoder, id uint64, cfg config, params json.RawMessage) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		writeError(enc, id, -32600, "invalid params", err.Error())
		return
	}
	if p.Name != cfg.Name {
		writeError(enc, id, -32602, "unknown tool", p.Name)
		return
	}
	var body json.RawMessage
	switch strings.ToLower(cfg.Behaviour) {
	case "echo":
		body = p.Arguments
	case "fixed":
		body = cfg.Fixed
	default:
		writeError(enc, id, -32603, "unknown behaviour", cfg.Behaviour)
		return
	}
	text := string(body)
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

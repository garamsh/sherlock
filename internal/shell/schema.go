package shell

import (
	_ "embed"
	"encoding/json"
)

// schemaFile is the embedded JSON Schema document for the shell_exec
// tool. It holds the input and output schemas side by side so the
// build-time conformance test (internal/tool/tooltest) can validate both
// halves against the same source of truth.
//
//go:embed schema.json
var schemaFile []byte

// schemaDoc mirrors the on-disk shape of schema.json. The raw messages
// keep the embedded bytes untouched straight through to the LLM-facing
// tool.Tool contract.
type schemaDoc struct {
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
}

// schema is the parsed view of schemaFile. A package-level parse keeps
// the embedded bytes a single source of truth and a single failure point
// if the file is malformed.
var schema = func() schemaDoc {
	var doc schemaDoc
	if err := json.Unmarshal(schemaFile, &doc); err != nil {
		panic("shell: embedded schema.json is malformed: " + err.Error())
	}
	return doc
}()

// InputSchema returns the JSON Schema for the shell_exec tool's input.
func InputSchema() json.RawMessage { return schema.Input }

// OutputSchema returns the JSON Schema for the shell_exec tool's output.
func OutputSchema() json.RawMessage { return schema.Output }

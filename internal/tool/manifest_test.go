package tool_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

// minimalManifest is the smallest manifest that parses and validates
// against the JSON Schema compiler. The empty-objects schema gives the
// platform a permissive contract that no model will trip.
const minimalManifest = `{
	"name": "echo",
	"description": "echo the input",
	"command": "echo.sh",
	"input_schema": {"type": "object"},
	"output_schema": {"type": "object"}
}`

// writeFile writes data to a path under t.TempDir and returns the path.
// Used to scaffold manifests in subdirectories.
func writeFile(t *testing.T, path, data string) error {
	t.Helper()
	return os.WriteFile(path, []byte(data), 0o600)
}

// TestParseManifest_OK covers the happy path: a well-formed manifest
// parses, the required fields are present, and the schemas are valid
// JSON Schema documents.
func TestParseManifest_OK(t *testing.T) {
	m, err := tool.ParseManifest([]byte(minimalManifest))
	require.NoError(t, err)
	assert.Equal(t, "echo", m.Name)
	assert.Equal(t, "echo the input", m.Description)
	assert.Equal(t, "echo.sh", m.Command)
	assert.Empty(t, m.Env)
	assert.JSONEq(t, `{"type":"object"}`, string(m.InputSchema))
}

// TestParseManifest_StrictFields pins the schema-level discipline: the
// parser rejects unknown fields so a typo in a manifest field name
// surfaces at startup instead of silently dropping the field.
func TestParseManifest_StrictFields(t *testing.T) {
	bad := strings.Replace(minimalManifest, `"description"`, `"desc"`, 1)
	_, err := tool.ParseManifest([]byte(bad))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse manifest")
}

// TestParseManifest_RejectsMalformedJSON covers the parseability check
// (ADR 0009 invariant 1). A truncated manifest must fail at startup.
func TestParseManifest_RejectsMalformedJSON(t *testing.T) {
	_, err := tool.ParseManifest([]byte(`{"name": "x"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse manifest")
}

// TestValidateManifest_RequiresName covers the structural rules: name
// and command must be non-empty. The platform fails startups that omit
// either field rather than ship a tool whose descriptor is incomplete.
// LoadManifest runs the full parse + validate pipeline.
func TestValidateManifest_RequiresName(t *testing.T) {
	_, err := tool.ValidateManifest(tool.Manifest{
		Description:  "no name",
		Command:      "x.sh",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

// TestValidateManifest_RequiresCommand is the symmetric check: the
// command path is required so the runner has something to spawn.
func TestValidateManifest_RequiresCommand(t *testing.T) {
	_, err := tool.ValidateManifest(tool.Manifest{
		Name:         "echo",
		Description:  "no command",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

// TestValidateManifest_InvalidInputSchema covers the schema validation:
// a non-schema document is rejected at startup so the platform never
// tries to compile it at call time.
func TestValidateManifest_InvalidInputSchema(t *testing.T) {
	_, err := tool.ValidateManifest(tool.Manifest{
		Name:         "echo",
		Description:  "d",
		Command:      "x.sh",
		InputSchema:  json.RawMessage(`{"type":42}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input_schema")
}

// TestValidateManifest_InvalidOutputSchema is the symmetric check on
// the output schema.
func TestValidateManifest_InvalidOutputSchema(t *testing.T) {
	_, err := tool.ValidateManifest(tool.Manifest{
		Name:         "echo",
		Description:  "d",
		Command:      "x.sh",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":42}`),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_schema")
}

// TestParseManifest_DefaultsMissingSchemas covers the default behavior:
// a manifest that omits input_schema or output_schema is accepted with
// the empty-object schema as the fallback. The platform treats the
// absence as permissive validation.
func TestParseManifest_DefaultsMissingSchemas(t *testing.T) {
	raw := []byte(`{
		"name": "echo",
		"description": "d",
		"command": "x.sh"
	}`)
	m, err := tool.ParseManifest(raw)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"object"}`, string(m.InputSchema))
	assert.JSONEq(t, `{"type":"object"}`, string(m.OutputSchema))
}

// TestParseManifest_EnvArray confirms the env field round-trips as a
// string slice. The runner forwards the array to the spawned process.
func TestParseManifest_EnvArray(t *testing.T) {
	raw := []byte(`{
		"name": "echo",
		"description": "d",
		"command": "x.sh",
		"env": ["FOO=bar", "BAZ=qux"],
		"input_schema": {"type": "object"},
		"output_schema": {"type": "object"}
	}`)
	m, err := tool.ParseManifest(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, m.Env)
}

// TestManifestHash_StableUnderKeyOrder pins the canonicalization rule:
// the hash is the SHA-256 of the canonical manifest bytes, so two
// manifests that differ only in field order hash to the same value.
func TestManifestHash_StableUnderKeyOrder(t *testing.T) {
	a := `{"name":"a","description":"d","command":"c","input_schema":{"type":"object"},"output_schema":{"type":"object"}}`
	b := `{"output_schema":{"type":"object"},"input_schema":{"type":"object"},"command":"c","description":"d","name":"a"}`
	assert.Equal(t, tool.ManifestHash([]byte(a)), tool.ManifestHash([]byte(b)),
		"the hash is over the canonical bytes; field order is not signal")
}

// TestManifestHash_StableUnderWhitespace covers the same canonical rule
// for whitespace: a tool author that reformats the JSON does not drift
// the pin.
func TestManifestHash_StableUnderWhitespace(t *testing.T) {
	minified := `{"name":"a","description":"d","command":"c","input_schema":{"type":"object"},"output_schema":{"type":"object"}}`
	pretty := `{
		"name": "a",
		"description": "d",
		"command": "c",
		"input_schema": {"type":"object"},
		"output_schema": {"type":"object"}
	}`
	assert.Equal(t, tool.ManifestHash([]byte(minified)), tool.ManifestHash([]byte(pretty)),
		"whitespace is not part of the hash")
}

// TestLoadManifest_ReadsFile exercises the file path through LoadManifest
// and confirms the parsed manifest matches the bytes on disk.
func TestLoadManifest_ReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	require.NoError(t, writeFile(t, path, minimalManifest))
	m, raw, err := tool.LoadManifest(path)
	require.NoError(t, err)
	assert.Equal(t, "echo", m.Name)
	assert.Equal(t, []byte(minimalManifest), raw)
}

// TestLoadManifest_FileMissing surfaces the read error path so the
// caller can replace the path or fail startup.
func TestLoadManifest_FileMissing(t *testing.T) {
	_, _, err := tool.LoadManifest(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

// TestLoadManifest_EnvArraySerialize pins the JSON round-trip: the
// env field is a string array, and the manifest deserializes it into
// the Manifest struct correctly.
func TestLoadManifest_EnvArraySerialize(t *testing.T) {
	doc := map[string]any{
		"name":          "t",
		"description":   "d",
		"command":       "x.sh",
		"env":           []string{"A=1", "B=2"},
		"input_schema":  map[string]any{"type": "object"},
		"output_schema": map[string]any{"type": "object"},
	}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	m, err := tool.ParseManifest(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"A=1", "B=2"}, m.Env)
}

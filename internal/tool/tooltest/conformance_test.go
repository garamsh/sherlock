package tooltest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// TestCheck_NonUTF8Fails covers the first build-time error: bytes that
// are not valid UTF-8. The cheapest source of a malformed file is a
// hand-edit that includes a stray high-bit byte; the conformance check
// must surface it.
func TestCheck_NonUTF8Fails(t *testing.T) {
	bad := []byte{'"', 0xff, 0xfe, '"'} // invalid UTF-8 inside a string

	err := tooltest.Check("nonutf8", bad)
	if err == nil {
		t.Fatal("Check accepted non-UTF-8 bytes")
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected UTF-8 error, got: %v", err)
	}
}

// TestCheck_InvalidJSONFails covers the second build-time error: JSON
// that does not parse. A trailing comma or an unclosed brace should fail
// the build, not be passed to the model.
func TestCheck_InvalidJSONFails(t *testing.T) {
	bad := []byte(`{"type": "object",`) // truncated

	err := tooltest.Check("broken", bad)
	if err == nil {
		t.Fatal("Check accepted malformed JSON")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("expected JSON error, got: %v", err)
	}
}

// TestCheck_InvalidJSONSchemaFails covers the third build-time error:
// JSON that parses but is not a valid JSON Schema document. The builtin
// validator catches a malformed draft so the model never sees it.
func TestCheck_InvalidJSONSchemaFails(t *testing.T) {
	// `type` must be a string or an array of strings; an integer is not
	// a valid JSON Schema "type" value.
	bad := []byte(`{"type": 42}`)

	err := tooltest.Check("badschema", bad)
	if err == nil {
		t.Fatal("Check accepted a non-schema JSON document")
	}
	if !strings.Contains(err.Error(), "JSON Schema") {
		t.Fatalf("expected JSON Schema error, got: %v", err)
	}
}

// TestCheck_RequiredReferencesMissingPropertyFails covers the fourth
// build-time error: a `required` entry pointing at a property that does
// not exist. The check walks recursively so the failure also triggers
// when the missing reference is in a nested `then` block.
func TestCheck_RequiredReferencesMissingPropertyFails(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "missing at root",
			schema: `{
				"type": "object",
				"properties": {"a": {"type": "string"}},
				"required": ["missing"]
			}`,
		},
		{
			name: "missing in nested then",
			schema: `{
				"type": "object",
				"properties": {"a": {"type": "string"}},
				"if": {"properties": {"a": {"const": "x"}}, "required": ["a"]},
				"then": {"properties": {"missing": {"type": "string"}}, "required": ["missing"]}
			}`,
		},
		{
			name: "missing in oneOf branch",
			schema: `{
				"oneOf": [
					{"type": "object", "properties": {"a": {"type": "string"}}, "required": ["missing"]}
				]
			}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tooltest.Check("missing", []byte(tc.schema))
			if err == nil {
				t.Fatal("Check accepted a required array referencing a missing property")
			}
			if !strings.Contains(err.Error(), "required/properties mismatch") {
				t.Fatalf("expected required/properties error, got: %v", err)
			}
		})
	}
}

// TestCheck_ThenInheritsParentProperties confirms the inheritance rule:
// a `then` block without its own `properties` may reference a property
// declared in the parent. JSON Schema applies `then` to the same
// instance, so the property is in scope. The files tool's input schema
// uses this pattern; the check must not reject it.
func TestCheck_ThenInheritsParentProperties(t *testing.T) {
	schema := []byte(`{
		"type": "object",
		"properties": {
			"operation": {"type": "string", "enum": ["read", "write"]},
			"content": {"type": "string"}
		},
		"required": ["operation"],
		"if": {"properties": {"operation": {"const": "write"}}, "required": ["operation"]},
		"then": {"required": ["content"]}
	}`)

	if err := tooltest.Check("files-style", schema); err != nil {
		t.Fatalf("Check rejected a valid then-inherits-parent schema: %v", err)
	}
}

// TestCheckSchema_AcceptsEveryBuiltinDocument drives each shipped
// schema through the t.Fatalf-style helper and confirms the test
// passes; this is the gate the issue calls out as the build-time guard.
func TestCheckSchema_AcceptsEveryBuiltinDocument(t *testing.T) {
	for _, c := range []struct {
		name string
		file string
	}{
		{"files", "cmd/files/schema.json"},
		{"github", "cmd/github/schema.json"},
		{"message", "internal/message/schema.json"},
		{"playwright", "tools/playwright/schema.json"},
		{"shell_exec", "cmd/shell_exec/schema.json"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tooltest.CheckSchema(t, c.name, mustRead(t, c.file))
		})
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	rel := filepath.Join(repoRoot(t), path)
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return data
}

// repoRoot returns the module root for the current test process. Files
// in test cases reference paths relative to the repo, not the test
// package's directory, so a single helper resolves the root once.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate module root from %s", dir)
		}
		dir = parent
	}
}

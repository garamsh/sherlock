package tooltest_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/tool/tooltest"
)

type shapeSample struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
	ignored int    //nolint:unused // unexported fields must not count
	Skipped string `json:"-"`
}

func TestCheckStructShape_MatchingSchemaPasses(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string"},
			"timeout": {"type": "integer"}
		},
		"required": ["command"]
	}`)
	tooltest.CheckStructShape(t, schema, reflect.TypeOf(shapeSample{}))
}

// TestCheckShape_DriftFails asserts the guard reports an error for every
// drift mode it exists to catch.
func TestCheckShape_DriftFails(t *testing.T) {
	typ := reflect.TypeOf(shapeSample{})
	tests := []struct {
		name   string
		schema string
	}{
		{
			name:   "property missing from struct",
			schema: `{"properties": {"command": {}, "timeout": {}, "extra": {}}}`,
		},
		{
			name:   "struct tag missing from schema",
			schema: `{"properties": {"command": {}}}`,
		},
		{
			name:   "required entry not a struct tag",
			schema: `{"properties": {"command": {}, "timeout": {}}, "required": ["bogus"]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tooltest.CheckShape(json.RawMessage(tc.schema), typ); err == nil {
				t.Fatalf("CheckShape accepted drifted schema %s", tc.schema)
			}
		})
	}
}

func TestSubSchemas_SplitsDocument(t *testing.T) {
	input, output := tooltest.SubSchemas(t, []byte(`{
		"input": {"type": "object", "properties": {"a": {}}},
		"output": {"type": "object"}
	}`))
	if !json.Valid(input) || !json.Valid(output) {
		t.Fatalf("SubSchemas returned invalid subschemas: input=%s output=%s", input, output)
	}
	var in struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(input, &in); err != nil || len(in.Properties) != 1 {
		t.Fatalf("SubSchemas input = %s, want the input subschema", input)
	}
}

package memory_test

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/memory/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "memory", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	// Round-trip the accessors so a future refactor that loses the embed
	// fails this test.
	if len(memory.InputSchema()) == 0 {
		t.Fatal("memory.InputSchema returned empty bytes")
	}
	if len(memory.OutputSchema()) == 0 {
		t.Fatal("memory.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match CallInput's
// json tags, and the output schema must match SearchOutput (ADR 0015
// shrank the tool to the single search op).
func TestSchema_MatchesStructTags(t *testing.T) {
	input, output := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(memory.CallInput{}))
	tooltest.CheckStructShape(t, output, reflect.TypeOf(memory.SearchOutput{}))

	// The search output nests the result-entry shape under results.items;
	// check it against SearchOutput's element type so the unexported entry
	// struct cannot drift either.
	var outDoc struct {
		Properties struct {
			Results struct {
				Items json.RawMessage `json:"items"`
			} `json:"results"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(output, &outDoc); err != nil {
		t.Fatalf("output schema does not parse: %v", err)
	}
	tooltest.CheckStructShape(t, outDoc.Properties.Results.Items,
		reflect.TypeOf(memory.SearchOutput{}.Results).Elem())
}

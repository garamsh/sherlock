package files_test

import (
	_ "embed"
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/files/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "files", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(files.InputSchema()) == 0 {
		t.Fatal("files.InputSchema returned empty bytes")
	}
	if len(files.OutputSchema()) == 0 {
		t.Fatal("files.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match CallInput's
// json tags. The output schema is an unconstrained object, so the
// Read/Write/ListOutput structs have no schema shape to check against.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, _ := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(files.CallInput{}))
}

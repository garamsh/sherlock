package shell_test

import (
	_ "embed"
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/shell/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "shell", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(shell.InputSchema()) == 0 {
		t.Fatal("shell.InputSchema returned empty bytes")
	}
	if len(shell.OutputSchema()) == 0 {
		t.Fatal("shell.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match CallInput's
// json tags, and the output schema must match Result's.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, output := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(shell.CallInput{}))
	tooltest.CheckStructShape(t, output, reflect.TypeOf(shell.Result{}))
}

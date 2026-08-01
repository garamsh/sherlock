package message_test

import (
	_ "embed"
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/message"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/message/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "message", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(message.InputSchema()) == 0 {
		t.Fatal("message.InputSchema returned empty bytes")
	}
	if len(message.OutputSchema()) == 0 {
		t.Fatal("message.OutputSchema returned empty bytes")
	}
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties/required must match Message's
// json tags, and the output schema must match CallOutput's.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, output := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(message.Message{}))
	tooltest.CheckStructShape(t, output, reflect.TypeOf(message.CallOutput{}))
}

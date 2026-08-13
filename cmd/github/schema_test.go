package main

import (
	"reflect"
	"testing"

	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// TestEmbeddedSchema_Conforms checks the schema against the build-time
// JSON Schema guard (UTF-8, JSON, JSON Schema, required/properties).
// schemaFile is the same byte slice main.go embeds via go:embed.
func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "github", schemaFile)
}

// TestSchema_MatchesStructTags is the build-time schema/struct drift
// guard: the input schema's properties must match callInput's json
// tags, and the output schema must match callOutput's.
func TestSchema_MatchesStructTags(t *testing.T) {
	input, output := tooltest.SubSchemas(t, schemaFile)
	tooltest.CheckStructShape(t, input, reflect.TypeOf(callInput{}))
	tooltest.CheckStructShape(t, output, reflect.TypeOf(callOutput{}))
}

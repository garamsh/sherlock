package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestSchema_AdvertisesPerOperationRequiredFields asserts the schema
// the model sees carries the same per-operation required fields the
// runtime validates against in handleCall. Issue #297 (allOf was
// stripped by the OpenAI provider's sanitization, hiding the
// conditional requirements from the model): the schema now uses
// oneOf to encode the branches, so the model sees the requirements
// before it emits a call.
func TestSchema_AdvertisesPerOperationRequiredFields(t *testing.T) {
	input, _ := tooltest.SubSchemas(t, schemaFile)
	var doc struct {
		OneOf []json.RawMessage `json:"oneOf"`
	}
	require.NoError(t, json.Unmarshal(input, &doc))

	expected := map[string][]string{
		"push":              {"operation", "branch", "sha"},
		"open_issue":        {"operation", "title"},
		"open_pull_request": {"operation", "head", "base", "title"},
		"comment":           {"operation", "number", "message"},
	}

	for _, branch := range doc.OneOf {
		var op struct {
			Properties map[string]struct {
				Const string `json:"const"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		require.NoError(t, json.Unmarshal(branch, &op))
		require.Contains(t, op.Properties, "operation",
			"each oneOf branch must pin an operation")
		want, ok := expected[op.Properties["operation"].Const]
		require.True(t, ok, "unknown operation in oneOf branch: %q", op.Properties["operation"].Const)
		assert.Equal(t, want, op.Required,
			"operation %q: schema required list must match the runtime's per-operation check",
			op.Properties["operation"].Const)
	}
}

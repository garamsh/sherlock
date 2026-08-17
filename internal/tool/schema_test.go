// This file is an internal test (stack-go.md §7's white-box seam) because
// validate and validateSchemaDocument have no exported form of their own.
// An external test reaches them only through Registry.Call or
// ValidateManifest, which would assert registry and manifest behaviour with
// schema validation as a side effect.
package tool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSchema_EmptySchemaAcceptsAnyInput(t *testing.T) {
	require.NoError(t, validate(json.RawMessage(nil), json.RawMessage(`{"anything":1}`)))
}

func TestValidateSchema_MatchingInputPasses(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"name":{"type":"string"}},
		"required":["name"]
	}`)
	require.NoError(t, validate(schema, json.RawMessage(`{"name":"ada"}`)))
}

func TestValidateSchema_MismatchedInputFails(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"name":{"type":"string"}},
		"required":["name"]
	}`)
	err := validate(schema, json.RawMessage(`{"name":42}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input does not match schema")
}

func TestValidateSchemaDocument_RejectsInvalidDocument(t *testing.T) {
	// `type` must be a string or array of strings; an integer is
	// not a valid JSON Schema "type" value.
	err := validateSchemaDocument(json.RawMessage(`{"type":42}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema does not parse")
}

func TestValidateSchemaDocument_AcceptsValidDocument(t *testing.T) {
	err := validateSchemaDocument(json.RawMessage(`{
		"type":"object",
		"properties":{"x":{"type":"integer"}}
	}`))
	require.NoError(t, err)
}

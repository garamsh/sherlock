package tool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/tool"
)

func TestValidateSchema_EmptySchemaAcceptsAnyInput(t *testing.T) {
	require.NoError(t, tool.ValidatePublic(json.RawMessage(nil), json.RawMessage(`{"anything":1}`)))
}

func TestValidateSchema_MatchingInputPasses(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"name":{"type":"string"}},
		"required":["name"]
	}`)
	require.NoError(t, tool.ValidatePublic(schema, json.RawMessage(`{"name":"ada"}`)))
}

func TestValidateSchema_MismatchedInputFails(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{"name":{"type":"string"}},
		"required":["name"]
	}`)
	err := tool.ValidatePublic(schema, json.RawMessage(`{"name":42}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "input does not match schema")
}

func TestValidateSchemaDocument_RejectsInvalidDocument(t *testing.T) {
	// `type` must be a string or array of strings; an integer is
	// not a valid JSON Schema "type" value.
	err := tool.ValidateSchemaDocumentPublic(json.RawMessage(`{"type":42}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema does not parse")
}

func TestValidateSchemaDocument_AcceptsValidDocument(t *testing.T) {
	err := tool.ValidateSchemaDocumentPublic(json.RawMessage(`{
		"type":"object",
		"properties":{"x":{"type":"integer"}}
	}`))
	require.NoError(t, err)
}

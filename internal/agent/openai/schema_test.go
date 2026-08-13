package openai

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quietLogger drops every log line so the sanitizer's warnings do not
// clutter test output. Restored via t.Cleanup.
func quietLogger(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
}

func TestSanitizeToolParameters_EmptyAndInsideSubsetPassThrough(t *testing.T) {
	quietLogger(t)
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{"nil", nil},
		{"empty", json.RawMessage("")},
		{"already_object", json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeToolParameters("echo", tc.raw)
			// Byte-equal: no transformation was needed.
			assert.Equal(t, string(tc.raw), string(got))
		})
	}
}

func TestSanitizeToolParameters_DropsForbiddenTopLevelKeywords(t *testing.T) {
	quietLogger(t)
	cases := []struct {
		name    string
		raw     string
		keyword string
	}{
		{"allOf", `{"allOf":[{"type":"object"}]}`, "allOf"},
		{"anyOf", `{"anyOf":[{"type":"object"}]}`, "anyOf"},
		{"not", `{"not":{"type":"string"}}`, "not"},
		{"enum", `{"enum":["a","b"]}`, "enum"},
		{"const", `{"const":"x"}`, "const"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeToolParameters("external_tool", json.RawMessage(tc.raw))
			var doc map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(got, &doc))
			assert.NotContains(t, doc, tc.keyword, "the forbidden keyword must be dropped")
			require.Contains(t, doc, "type", "the sanitized schema must still have a type")
			assert.JSONEq(t, `"object"`, string(doc["type"]))
		})
	}
}

func TestSanitizeToolParameters_CoercesNonObjectRoot(t *testing.T) {
	quietLogger(t)
	cases := []struct {
		name string
		raw  string
	}{
		{"null_root", `null`},
		{"string_root", `"hello"`},
		{"array_root", `[1,2,3]`},
		{"missing_type", `{"properties":{"text":{"type":"string"}}}`},
		{"wrong_type", `{"type":"string"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeToolParameters("external_tool", json.RawMessage(tc.raw))
			var doc map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(got, &doc))
			assert.JSONEq(t, `"object"`, string(doc["type"]))
		})
	}
}

func TestSanitizeToolParameters_DropsMultipleForbiddenKeywords(t *testing.T) {
	quietLogger(t)
	raw := json.RawMessage(`{
		"allOf": [{"type":"object"}],
		"oneOf": [{"type":"string"}],
		"anyOf": [{"type":"number"}]
	}`)
	got := sanitizeToolParameters("external_tool", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))
	for _, kw := range OpenAITopLevelForbidden {
		assert.NotContains(t, doc, kw, "%q should be dropped", kw)
	}
	assert.JSONEq(t, `"object"`, string(doc["type"]))
	assert.Contains(t, doc, "oneOf", "top-level oneOf is supported in strict mode and must be preserved")
}

func TestSanitizeToolParameters_PreservesNestedKeywords(t *testing.T) {
	quietLogger(t)
	// The sanitizer only acts on top-level keywords; nested ones (e.g.
	// inside properties) stay put.
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"choice":{"oneOf":[{"type":"string"},{"type":"number"}]}
		}
	}`)
	got := sanitizeToolParameters("external_tool", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))
	var props map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(doc["properties"], &props))
	assert.Contains(t, props, "choice", "nested properties.choice must survive")
	var choice map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(props["choice"], &choice))
	assert.Contains(t, choice, "oneOf", "nested oneOf must be preserved")
	assert.JSONEq(t, `"object"`, string(doc["type"]))
}

// TestSanitizeToolParameters_PreservesTopLevelOneOf asserts the strict
// subset keeps oneOf at the top level so per-operation branches (issue
// #297) survive sanitization.
func TestSanitizeToolParameters_PreservesTopLevelOneOf(t *testing.T) {
	quietLogger(t)
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{"operation":{"type":"string"}},
		"required":["operation"],
		"oneOf":[
			{"properties":{"operation":{"const":"a"}},"required":["operation"]},
			{"properties":{"operation":{"const":"b"}},"required":["operation"]}
		]
	}`)
	got := sanitizeToolParameters("gh", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))
	assert.Contains(t, doc, "oneOf", "top-level oneOf must survive sanitization")
}

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
		{"oneOf", `{"oneOf":[{"type":"object"},{"type":"string"}]}`, "oneOf"},
		{"allOf", `{"allOf":[{"type":"object"}]}`, "allOf"},
		{"anyOf", `{"anyOf":[{"type":"object"}]}`, "anyOf"},
		{"not", `{"not":{"type":"string"}}`, "not"},
		{"enum", `{"enum":["a","b"]}`, "enum"},
		{"const", `{"const":"x"}`, "const"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeToolParameters("mcp_tool", json.RawMessage(tc.raw))
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
			got := sanitizeToolParameters("mcp_tool", json.RawMessage(tc.raw))
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
	got := sanitizeToolParameters("mcp_tool", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))
	for _, kw := range OpenAITopLevelForbidden {
		if kw == "not" || kw == "enum" || kw == "const" {
			continue
		}
		assert.NotContains(t, doc, kw, "%q should be dropped", kw)
	}
	assert.JSONEq(t, `"object"`, string(doc["type"]))
}

func TestSanitizeToolParameters_PreservesNestedKeywords(t *testing.T) {
	quietLogger(t)
	// The API only forbids forbidden keywords at the top level; nested ones
	// (e.g. inside properties) stay put.
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"choice":{"oneOf":[{"type":"string"},{"type":"number"}]}
		}
	}`)
	got := sanitizeToolParameters("mcp_tool", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))
	assert.NotContains(t, doc, "oneOf", "top-level oneOf must not appear")
	var props map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(doc["properties"], &props))
	assert.Contains(t, props, "choice", "nested properties.choice must survive")
	var choice map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(props["choice"], &choice))
	assert.Contains(t, choice, "oneOf", "nested oneOf must be preserved")
	assert.JSONEq(t, `"object"`, string(doc["type"]))
}

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
		{"already_object", json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)},
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
		{"oneOf", `{"oneOf":[{"type":"string"}]}`, "oneOf"},
		{"not", `{"not":{"type":"string"}}`, "not"},
		{"enum", `{"enum":["a","b"]}`, "enum"},
		{"const", `{"const":"x"}`, "const"},
		{"if", `{"if":{"type":"string"}}`, "if"},
		{"then", `{"then":{"type":"string"}}`, "then"},
		{"else", `{"else":{"type":"string"}}`, "else"},
		{"$ref", `{"$ref":"#/definitions/x"}`, "$ref"},
		{"dependentSchemas", `{"dependentSchemas":{"x":["y"]}}`, "dependentSchemas"},
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

// TestSanitizeToolParameters_ForbiddenListMatchesExporter pins the
// contract between sanitizeSuite and the exported list: every keyword
// listed in OpenAITopLevelForbidden is reachable through the
// sanitizer's drop loop, so future edits to either side stay in sync.
// Issues #313, #314.
func TestSanitizeToolParameters_ForbiddenListMatchesExporter(t *testing.T) {
	quietLogger(t)
	expected := []string{
		"allOf", "anyOf", "oneOf", "not", "enum", "const",
		"if", "then", "else", "$ref", "dependentSchemas",
	}
	assert.Equal(t, expected, OpenAITopLevelForbidden,
		"the forbidden-keyword list must include every entry covered by the suite")
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
		"anyOf": [{"type":"number"}],
		"if": {"type":"string"},
		"$ref": "#/definitions/x"
	}`)
	got := sanitizeToolParameters("external_tool", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))
	for _, kw := range OpenAITopLevelForbidden {
		assert.NotContains(t, doc, kw, "%q should be dropped", kw)
	}
	assert.JSONEq(t, `"object"`, string(doc["type"]))
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

// TestSanitizeToolParameters_DropsTopLevelOneOf pins the strict-mode
// posture for issue #313: the chat-completions strict-mode API rejects
// top-level oneOf, so the sanitizer must drop the keyword and keep the
// unconditional type/required/properties the API does accept. The
// previous TestSanitizeToolParameters_PreservesTopLevelOneOf enshrined
// the wrong assumption (#314) and was replaced by this one.
func TestSanitizeToolParameters_DropsTopLevelOneOf(t *testing.T) {
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
	assert.NotContains(t, doc, "oneOf",
		"top-level oneOf is rejected by the strict-mode API (issue #313) and must be dropped")
	assert.JSONEq(t, `"object"`, string(doc["type"]))
	assert.Contains(t, doc, "properties", "the unconditional structural fields must survive sanitization")
	assert.Contains(t, doc, "required", "the unconditional required list must survive sanitization")
}

// TestSanitizeToolParameters_PromotesMissingRequiredToNullable pins the
// strict-mode required ⊇ properties rule for issue #317: the
// chat-completions strict-mode API rejects a parameters document whose
// `properties` lists keys absent from `required`. For every missing
// key, the sanitizer must promote the key into `required` and rewrite
// its `type` from a single primitive (`"string"`) to `["<original>",
// "null"]` so the model can emit null when there is no value to send.
// The first-party `files` and `github` manifests violate the rule; the
// rewrite is the boundary posture (runtime-safety.md §Trust boundaries).
func TestSanitizeToolParameters_PromotesMissingRequiredToNullable(t *testing.T) {
	quietLogger(t)
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"operation":{"type":"string"},
			"path":{"type":"string"},
			"content":{"type":"string"}
		},
		"required":["operation","path"],
		"additionalProperties":false
	}`)
	got := sanitizeToolParameters("files", raw)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &doc))

	var requiredList []string
	require.NoError(t, json.Unmarshal(doc["required"], &requiredList),
		"the rewritten schema must still carry a required list")
	assert.ElementsMatch(t, []string{"operation", "path", "content"}, requiredList,
		"every key in properties must appear in required (issue #317)")

	var props map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(doc["properties"], &props))
	var content map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(props["content"], &content))
	assert.JSONEq(t, `["string", "null"]`, string(content["type"]),
		"the promoted property's type must become [<original>, 'null']")

	var operation map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(props["operation"], &operation))
	assert.JSONEq(t, `"string"`, string(operation["type"]),
		"properties already in required must not be touched")
}

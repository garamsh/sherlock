package openai

import (
	"bytes"
	"encoding/json"
	"log/slog"
)

// OpenAITopLevelForbidden lists the JSON Schema keywords the OpenAI
// chat-completions strict-mode API rejects at the top level of a tool's
// parameters document. allOf, anyOf, not, enum, and const are
// unsupported; oneOf is supported in strict mode and is therefore
// preserved. The API also requires top-level type "object" (enforced
// separately inside sanitizeToolParameters). Issue #54/#55; updated for
// strict mode in #298.
var OpenAITopLevelForbidden = []string{"allOf", "anyOf", "not", "enum", "const"}

// emptyObjectParameters is the fallback parameters document when a tool's
// schema is unusable: not parseable as an object, or root-typed as something
// other than "object". It advertises an empty object so the model sees a
// consistent shape.
var emptyObjectParameters = json.RawMessage(`{"type":"object"}`)

// sanitizeToolParameters reduces a tool's input schema to the subset the
// OpenAI function-calling API accepts. Schemas already inside the subset
// are returned unchanged. Out-of-subset schemas are reduced: the root is
// forced to "object" and any of OpenAITopLevelForbidden at the top level
// are dropped. Each transformation is logged once at warn level so
// operators can spot upstream tools whose schemas violate the constraint.
//
// The original schema is preserved on the tool: runtime validation in
// internal/tool still uses t.InputSchema(), so the model's reduced view does
// not weaken the tool's actual contract.
func sanitizeToolParameters(name string, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		// nil doc happens when raw is the JSON literal "null".
		slog.Warn("openai provider: tool schema coerced to empty object",
			"op", "sanitize_tool_parameters",
			"tool", name,
			"reason", "not_a_json_object")
		return emptyObjectParameters
	}

	changed := false
	if !bytes.Equal(bytes.TrimSpace(doc["type"]), []byte(`"object"`)) {
		doc["type"] = json.RawMessage(`"object"`)
		changed = true
		slog.Warn("openai provider: tool schema root type coerced to object",
			"op", "sanitize_tool_parameters",
			"tool", name)
	}
	for _, kw := range OpenAITopLevelForbidden {
		if _, ok := doc[kw]; ok {
			delete(doc, kw)
			changed = true
			slog.Warn("openai provider: tool schema dropped top-level keyword",
				"op", "sanitize_tool_parameters",
				"tool", name,
				"keyword", kw)
		}
	}
	if !changed {
		return raw
	}
	out, err := json.Marshal(doc)
	if err != nil {
		// json.Marshal of map[string]json.RawMessage cannot fail in practice
		// (raw messages are valid JSON), but fall back to the empty object
		// rather than panic.
		slog.Warn("openai provider: tool schema re-marshal failed; coerced to empty object",
			"op", "sanitize_tool_parameters",
			"tool", name,
			"error", err.Error())
		return emptyObjectParameters
	}
	return out
}

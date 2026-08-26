package openai

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sort"
)

// OpenAITopLevelForbidden lists every JSON Schema keyword the OpenAI
// chat-completions strict-mode API rejects at the top level of a tool's
// parameters document. The strict-mode subset does not accept
// composition (allOf, anyOf, oneOf), negation (not), enumerations
// (enum), constants (const), conditional schemas (if, then, else),
// references ($ref), or dependent schemas (dependentSchemas); the
// sanitizer drops every entry here so the API never sees them. The API
// also requires top-level type "object" (enforced separately inside
// sanitizeToolParameters). Every keyword is here because the API
// rejected it: issues #49, #55, #297 and #298 are the reports behind
// the list, and #313 is why oneOf is on it after PR #307 took it off.
// Keeping the list current with the API guards every future tool
// against the same regression.
var OpenAITopLevelForbidden = []string{
	"allOf",
	"anyOf",
	"oneOf",
	"not",
	"enum",
	"const",
	"if",
	"then",
	"else",
	"$ref",
	"dependentSchemas",
}

// emptyObjectParameters is the fallback parameters document when a tool's
// schema is unusable: not parseable as an object, or root-typed as something
// other than "object". It advertises an empty object so the model sees a
// consistent shape.
var emptyObjectParameters = json.RawMessage(`{"type":"object"}`)

// sanitizeToolParameters reduces a tool's input schema to the subset the
// OpenAI function-calling API accepts. Schemas already inside the subset
// are returned unchanged. Out-of-subset schemas are reduced: the root is
// forced to "object", any of OpenAITopLevelForbidden at the top level are
// dropped, and every property in `properties` is enforced to also appear
// in `required` (a missing entry is promoted to required and its type is
// rewritten to `["<original>", "null"]` so the model can emit null when
// there is no value to send). Each transformation is logged once at warn
// level so operators can spot upstream tools whose schemas violate the
// constraint.
//
// The reduction is this provider's alone. It is applied where the wire
// request is built (toWireTools) and nowhere else, so internal/tool
// keeps validating against the manifest's own schemas and the model's
// reduced view does not weaken the tool's actual contract. The one
// place the two meet is the null this function's promotion invites the
// model to emit, which the boundary reads as absence.
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
	if promoteRequiredSuperset(name, doc) {
		changed = true
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

// promoteRequiredSuperset enforces the strict-mode rule that every key in
// `properties` must also appear in `required`. For each property missing
// from `required`, the key is appended and its `type` is rewritten to
// `["<original>", "null"]` so the model can emit null when there is no
// value to send. Returns true when any property was promoted.
//
// The original required list's order is preserved; new entries are
// appended in sorted key order so the rewritten schema is deterministic
// across runs (Go map iteration is randomized).
func promoteRequiredSuperset(name string, doc map[string]json.RawMessage) bool {
	propsRaw, ok := doc["properties"]
	if !ok {
		return false
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(propsRaw, &props); err != nil || len(props) == 0 {
		return false
	}

	var requiredList []string
	if reqRaw, ok := doc["required"]; ok {
		_ = json.Unmarshal(reqRaw, &requiredList)
	}
	requiredSet := make(map[string]bool, len(requiredList))
	for _, r := range requiredList {
		requiredSet[r] = true
	}

	var missing []string
	for k := range props {
		if !requiredSet[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		return false
	}
	sort.Strings(missing)

	promoted := false
	for _, k := range missing {
		requiredList = append(requiredList, k)
		requiredSet[k] = true
		if updated, ok := rewritePropertyTypeAsNullable(props[k]); ok {
			props[k] = updated
		}
		promoted = true
		slog.Warn("openai provider: tool schema property promoted to required",
			"op", "sanitize_tool_parameters",
			"tool", name,
			"property", k)
	}

	if promoted {
		if updated, err := json.Marshal(requiredList); err == nil {
			doc["required"] = updated
		}
		if updated, err := json.Marshal(props); err == nil {
			doc["properties"] = updated
		}
	}
	return promoted
}

// rewritePropertyTypeAsNullable rewrites a property's `type` field from
// a single-type string (`"string"`) to `["<original>", "null"]`, leaving
// array forms (already nullable), properties without a type, and
// unparseable properties unchanged. Returns the rewritten raw and true
// when a rewrite happened.
func rewritePropertyTypeAsNullable(propRaw json.RawMessage) (json.RawMessage, bool) {
	var prop map[string]json.RawMessage
	if err := json.Unmarshal(propRaw, &prop); err != nil {
		return propRaw, false
	}
	typeRaw, ok := prop["type"]
	if !ok {
		return propRaw, false
	}
	trimmed := bytes.TrimSpace(typeRaw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		prop["type"] = json.RawMessage(`[` + string(typeRaw) + `, "null"]`)
		updated, err := json.Marshal(prop)
		if err != nil {
			return propRaw, false
		}
		return updated, true
	}
	return propRaw, false
}

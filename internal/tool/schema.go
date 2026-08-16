package tool

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaResourceURI names the compiled schema resource. It is absolute and
// cwd-independent so validation errors never embed a host filesystem path.
const schemaResourceURI = "https://gagent.local/schema/tool-input.json"

// ValidatePublic exposes the validate function for tests.
func ValidatePublic(schema json.RawMessage, input json.RawMessage) error {
	return validate(schema, input)
}

// ValidateSchemaDocumentPublic exposes the validateSchemaDocument
// function for tests.
func ValidateSchemaDocumentPublic(schema json.RawMessage) error {
	return validateSchemaDocument(schema)
}

// validate checks input against a JSON Schema document. An empty schema
// accepts any input. The schema is compiled with the same library the
// tool runtime uses (santhosh-tekuri/jsonschema/v6), so the platform's
// boundary check matches the tool's own.
func validate(schema json.RawMessage, input json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	compiled, err := compileSchema(schema)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(input))
	if err != nil {
		return fmt.Errorf("parse input: %w", err)
	}
	if err := compiled.Validate(instance); err != nil {
		return fmt.Errorf("input does not match schema: %w", err)
	}
	return nil
}

// absentOptionalNulls drops every top-level key of input whose value is
// JSON null and which schema declares in `properties` but omits from
// `required`, returning the rewritten document. Input that is not a
// JSON object, and any key the rule does not name, is returned
// untouched.
//
// The strict-mode function-calling API has no way to declare an
// optional property, so a provider projecting a manifest for the model
// promotes every optional property into `required` with a nullable type
// and the model then emits null where it has no value to send (issue
// #319). Absence is what the manifest already permits for exactly these
// properties, so reading that null as absence is what lets the boundary
// hold the manifest (ADR 0009 invariant 3) without rejecting a call the
// model was told to make.
func absentOptionalNulls(schema json.RawMessage, input json.RawMessage) json.RawMessage {
	optional := optionalProperties(schema)
	if len(optional) == 0 {
		return input
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(input, &doc); err != nil || doc == nil {
		return input
	}
	dropped := false
	for key, value := range doc {
		if !optional[key] || !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		delete(doc, key)
		dropped = true
	}
	if !dropped {
		return input
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return input
	}
	return out
}

// optionalProperties returns the top-level property names schema
// declares and does not require.
func optionalProperties(schema json.RawMessage) map[string]bool {
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return nil
	}
	required := make(map[string]bool, len(doc.Required))
	for _, name := range doc.Required {
		required[name] = true
	}
	optional := make(map[string]bool, len(doc.Properties))
	for name := range doc.Properties {
		if !required[name] {
			optional[name] = true
		}
	}
	return optional
}

// validateSchemaDocument checks that the schema itself is a valid JSON
// Schema document (compile-clean). The platform re-validates every
// fetched descriptor at the boundary (ADR 0017 §Tool contract) before
// trusting the schema for input validation.
func validateSchemaDocument(schema json.RawMessage) error {
	_, err := compileSchema(schema)
	if err != nil {
		return fmt.Errorf("schema does not parse: %w", err)
	}
	return nil
}

// compileSchema loads and compiles a JSON Schema document. The empty
// schema compiles to nothing — callers handle the empty case.
func compileSchema(schema json.RawMessage) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaResourceURI, doc); err != nil {
		return nil, fmt.Errorf("load schema: %w", err)
	}
	return c.Compile(schemaResourceURI)
}

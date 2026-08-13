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

// validateSchemaDocument checks that the schema itself is a valid JSON
// Schema document (compile-clean). The platform re-validates every
// fetched descriptor at the boundary (ADR 0018 §Tool contract) before
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

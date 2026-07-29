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

// validate checks input against a JSON Schema document. An empty schema
// accepts any input.
func validate(schema json.RawMessage, input json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return fmt.Errorf("parse schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaResourceURI, doc); err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	compiled, err := c.Compile(schemaResourceURI)
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
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

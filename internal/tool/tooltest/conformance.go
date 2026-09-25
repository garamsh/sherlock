package tooltest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaResourceURI names the compiled schema resource. It is absolute and
// cwd-independent so failure messages never embed a host filesystem path,
// matching the internal/tool validation convention.
const schemaResourceURI = "https://sherlock.local/schema/tooltest.json"

// CheckSchema inspects a tool's JSON Schema document and fails the test on
// the four build-time errors the issue calls out: non-UTF-8 input,
// invalid JSON, invalid JSON Schema document, and a `required` array
// referencing a property that does not exist in the data instance being
// validated. A `required` entry refers to the enclosing data instance:
// combinators (if/then/else/allOf/oneOf/anyOf/not/dependentSchemas)
// inherit the parent's properties, while `properties` and
// `items.properties` open a new scope. The check recurses into nested
// schemas so a missing-property typo anywhere in the document is caught.
//
// The name parameter is the tool name; it appears in failure messages so
// the failing schema is identifiable without reading the file.
func CheckSchema(t *testing.T, name string, schema []byte) {
	t.Helper()
	if err := Check(name, schema); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// Check is the error-returning variant of CheckSchema. Tests that need to
// distinguish failure modes (or assert that a specific bad input fails)
// use Check directly and inspect the returned error.
func Check(name string, schema []byte) error {
	if !utf8.Valid(schema) {
		return errors.New("schema is not valid UTF-8")
	}
	if !json.Valid(schema) {
		return errors.New("schema is not valid JSON")
	}
	if err := compileSchema(schema); err != nil {
		return fmt.Errorf("schema does not parse as JSON Schema: %w", err)
	}
	if err := checkRequiredReferences(schema); err != nil {
		return fmt.Errorf("schema required/properties mismatch: %w", err)
	}
	return nil
}

// compileSchema loads the schema as a JSON Schema document. It catches
// malformed drafts, unsupported keywords, and unresolved references so
// the conformance guard fails on whatever the runtime validator would
// reject later.
func compileSchema(schema []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaResourceURI, doc); err != nil {
		return fmt.Errorf("add resource: %w", err)
	}
	if _, err := c.Compile(schemaResourceURI); err != nil {
		return fmt.Errorf("compile: %w", err)
	}
	return nil
}

// checkRequiredReferences asserts each name in `required` refers to a
// property of the enclosing data instance, recursing into nested schemas.
// Missing references are silent wire-format rot the runtime would only
// surface as a confusing validation error during a model call.
func checkRequiredReferences(schema []byte) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(schema, &doc); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	props, _ := decodeProperties(doc["properties"])
	return validateRequiredRefs("", doc, props)
}

// decodeProperties returns the property name set of a `properties` block,
// or nil if the input is missing or malformed. A nil set means "the
// enclosing data instance has no declared properties".
func decodeProperties(raw json.RawMessage) (map[string]struct{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(raw, &properties); err != nil {
		return nil, fmt.Errorf("properties is not an object: %w", err)
	}
	out := make(map[string]struct{}, len(properties))
	for k := range properties {
		out[k] = struct{}{}
	}
	return out, nil
}

// validateRequiredRefs recurses through schema objects, enforcing the
// required/properties invariant at every level. The path argument is a
// JSON-Pointer-ish location so the error message points to the offending
// nested block rather than the file root. props is the current data
// instance's property set; it is replaced when a nested `properties`
// or `items.properties` block defines a new shape.
func validateRequiredRefs(path string, obj map[string]json.RawMessage, props map[string]struct{}) error {
	if raw, ok := obj["required"]; ok {
		if err := checkRequired(path, raw, props); err != nil {
			return err
		}
	}

	// Schema combinators apply to the same data instance as their parent,
	// so the property set is inherited unchanged.
	for _, key := range []string{
		"if", "then", "else",
		"allOf", "anyOf", "oneOf",
		"not", "dependentSchemas",
	} {
		if raw, ok := obj[key]; ok {
			if err := walkNested(path+"/"+key, raw, props); err != nil {
				return err
			}
		}
	}

	// Array items describe a fresh data instance: each item has the
	// properties declared under items.properties.
	if itemsRaw, ok := obj["items"]; ok {
		var items map[string]json.RawMessage
		if err := json.Unmarshal(itemsRaw, &items); err == nil {
			itemProps, _ := decodeProperties(items["properties"])
			if err := validateRequiredRefs(path+"/items", items, itemProps); err != nil {
				return err
			}
		}
	}

	// Each declared property's value is a schema describing that
	// property's value; recurse into it with the parent's properties as
	// the current scope (the property's value is not a new data instance).
	if propsRaw, ok := obj["properties"]; ok {
		var properties map[string]json.RawMessage
		if err := json.Unmarshal(propsRaw, &properties); err == nil {
			for k, v := range properties {
				var nested map[string]json.RawMessage
				if err := json.Unmarshal(v, &nested); err == nil && len(nested) > 0 {
					if err := validateRequiredRefs(path+"/properties/"+k, nested, props); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

// walkNested recurses into a keyword whose value is either a single
// schema object (if/then/else/not/dependentSchemas) or an array of
// schemas (allOf/anyOf/oneOf). Each inner schema applies to the same
// data instance as the parent, so the property set is inherited.
func walkNested(path string, raw json.RawMessage, props map[string]struct{}) error {
	var single map[string]json.RawMessage
	if err := json.Unmarshal(raw, &single); err == nil && len(single) > 0 {
		return validateRequiredRefs(path, single, props)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	for i, item := range arr {
		var itemObj map[string]json.RawMessage
		if err := json.Unmarshal(item, &itemObj); err != nil {
			continue
		}
		if err := validateRequiredRefs(fmt.Sprintf("%s[%d]", path, i), itemObj, props); err != nil {
			return err
		}
	}
	return nil
}

// checkRequired asserts every name in `required` is present in props.
// An empty props with a non-empty `required` is a typo: there is no
// data instance property for the constraint to refer to.
func checkRequired(path string, requiredRaw json.RawMessage, props map[string]struct{}) error {
	var required []string
	if err := json.Unmarshal(requiredRaw, &required); err != nil {
		return fmt.Errorf("%s: required is not a string array: %w", path, err)
	}
	if props == nil {
		if len(required) > 0 {
			return fmt.Errorf("%s: required entry %q has no enclosing properties", path, required[0])
		}
		return nil
	}
	for _, r := range required {
		if _, ok := props[r]; !ok {
			return fmt.Errorf("%s: required entry %q not in properties", path, r)
		}
	}
	return nil
}

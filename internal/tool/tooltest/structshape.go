package tooltest

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// SubSchemas splits a tool schema document into its input and output
// subschema documents so tests can check each against its typed struct.
func SubSchemas(t *testing.T, schema []byte) (input, output json.RawMessage) {
	t.Helper()
	var doc struct {
		Input  json.RawMessage `json:"input"`
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		t.Fatalf("schema document does not parse: %v", err)
	}
	return doc.Input, doc.Output
}

// CheckStructShape is a build-time guard against schema/struct drift: it
// fails the test unless the JSON Schema object in schemaJSON declares
// exactly the properties the struct typ exposes as json tags, and every
// required entry is one of those tags. typ is usually obtained via
// reflect.TypeOf(T{}).
func CheckStructShape(t *testing.T, schemaJSON json.RawMessage, typ reflect.Type) {
	t.Helper()
	if err := CheckShape(schemaJSON, typ); err != nil {
		t.Fatal(err)
	}
}

// CheckShape is the error-returning variant of CheckStructShape. Tests
// that need to assert a specific drift fails use CheckShape directly and
// inspect the returned error.
func CheckShape(schemaJSON json.RawMessage, typ reflect.Type) error {
	if typ.Kind() != reflect.Struct {
		return fmt.Errorf("%v is not a struct type", typ)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return fmt.Errorf("%s: schema object does not parse: %w", typ.Name(), err)
	}

	tags := structTags(typ)
	props := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		props = append(props, name)
	}
	sort.Strings(tags)
	sort.Strings(props)
	if !slices.Equal(tags, props) {
		return fmt.Errorf("%s: schema properties %v do not match struct json tags %v", typ.Name(), props, tags)
	}
	for _, r := range schema.Required {
		if !slices.Contains(tags, r) {
			return fmt.Errorf("%s: required entry %q is not a struct json tag", typ.Name(), r)
		}
	}
	return nil
}

// structTags returns the json tag names of typ's fields, skipping fields
// without a tag or tagged "-".
func structTags(typ reflect.Type) []string {
	tags := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		tags = append(tags, name)
	}
	return tags
}

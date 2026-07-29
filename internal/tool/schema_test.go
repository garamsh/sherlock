package tool

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const personSchema = `{
	"type": "object",
	"properties": {"name": {"type": "string"}},
	"required": ["name"]
}`

func TestValidate_EmptySchemaAcceptsAnyInput(t *testing.T) {
	if err := validate(nil, json.RawMessage(`{"anything": 1}`)); err != nil {
		t.Fatalf("expected empty schema to accept input, got %v", err)
	}
}

func TestValidate_ValidInputPasses(t *testing.T) {
	if err := validate(json.RawMessage(personSchema), json.RawMessage(`{"name": "ada"}`)); err != nil {
		t.Fatalf("expected valid input to pass, got %v", err)
	}
}

func TestValidate_InvalidInputFails(t *testing.T) {
	if err := validate(json.RawMessage(personSchema), json.RawMessage(`{"name": 42}`)); err == nil {
		t.Fatal("expected invalid input to fail validation")
	}
}

func TestValidate_ErrorOmitsHostFilesystemPath(t *testing.T) {
	err := validate(json.RawMessage(personSchema), json.RawMessage(`{"name": 42}`))
	if err == nil {
		t.Fatal("expected invalid input to fail validation")
	}
	msg := err.Error()
	if strings.Contains(msg, "file://") {
		t.Errorf("validation error leaks a file URL: %q", msg)
	}
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatalf("get working directory: %v", cwdErr)
	}
	if strings.Contains(msg, cwd) {
		t.Errorf("validation error leaks the working directory %q: %q", cwd, msg)
	}
}

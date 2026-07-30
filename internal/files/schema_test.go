package files_test

import (
	_ "embed"
	"testing"

	"github.com/garamsh/gagent/internal/files"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/files/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "files", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(files.InputSchema()) == 0 {
		t.Fatal("files.InputSchema returned empty bytes")
	}
	if len(files.OutputSchema()) == 0 {
		t.Fatal("files.OutputSchema returned empty bytes")
	}
}

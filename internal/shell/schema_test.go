package shell_test

import (
	_ "embed"
	"testing"

	"github.com/garamsh/gagent/internal/shell"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/shell/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "shell", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	if len(shell.InputSchema()) == 0 {
		t.Fatal("shell.InputSchema returned empty bytes")
	}
	if len(shell.OutputSchema()) == 0 {
		t.Fatal("shell.OutputSchema returned empty bytes")
	}
}

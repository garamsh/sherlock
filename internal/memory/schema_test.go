package memory_test

import (
	_ "embed"
	"testing"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/tool/tooltest"
)

// schemaFile mirrors the embed in internal/memory/schema.go so the test
// reads the same shipped bytes the runtime serves.
//
//go:embed schema.json
var schemaFile []byte

func TestEmbeddedSchema_Conforms(t *testing.T) {
	tooltest.CheckSchema(t, "memory", schemaFile)
}

func TestToolSchema_AccessorsReturnEmbeddedBytes(t *testing.T) {
	// Round-trip the accessors so a future refactor that loses the embed
	// fails this test.
	if len(memory.InputSchema()) == 0 {
		t.Fatal("memory.InputSchema returned empty bytes")
	}
	if len(memory.OutputSchema()) == 0 {
		t.Fatal("memory.OutputSchema returned empty bytes")
	}
}

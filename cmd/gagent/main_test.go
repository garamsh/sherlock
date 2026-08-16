package main

import (
	"os"
	"testing"

	"github.com/garamsh/gagent/internal/workspace/shell"
)

// TestMain routes the resource-limit verb the way main does: the
// workspace tests here start a real executor, which execs the binary it
// runs as — this test binary — to cap each untrusted child (ADR 0008).
func TestMain(m *testing.M) {
	shell.ShimIfRequested()
	os.Exit(m.Run())
}

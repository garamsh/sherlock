package shell_test

import (
	"os"
	"testing"

	"github.com/garamsh/sherlock/internal/workspace/shell"
)

// TestMain lets this test binary stand in for the platform binary: the
// executor execs whichever binary it runs as, as the resource-limit
// shim for every untrusted child (ADR 0008), and under `go test` that
// is this one.
func TestMain(m *testing.M) {
	shell.ShimIfRequested()
	os.Exit(m.Run())
}

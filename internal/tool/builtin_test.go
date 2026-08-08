package tool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/gagent/internal/tool"
)

// TestBuiltinToolNames pins the canonical names of the sidecar's
// built-in tools (ADR 0018). A regression — a renamed tool, a missing
// entry — is a public contract change; this test is the gate the
// release notes cite.
func TestBuiltinToolNames(t *testing.T) {
	assert.Equal(t,
		[]string{"files", "shell_exec"},
		tool.BuiltinToolNames(),
		"the sidecar ships exactly two built-in tools")
}

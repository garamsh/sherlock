package tool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/gagent/internal/tool"
)

// TestBuiltinToolNames pins the canonical names of the four built-in
// tools (ADR 0012). A regression — a renamed tool, a missing entry — is
// a public contract change; this test is the gate the release notes cite.
func TestBuiltinToolNames(t *testing.T) {
	assert.Equal(t,
		[]string{"files", "memory", "message_send", "shell_exec"},
		tool.BuiltinToolNames(),
		"the platform ships exactly four built-in tools")
}

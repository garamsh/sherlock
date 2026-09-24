package agent_test

import (
	"strings"
	"testing"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeInstructionsKeepsThePlatformContract(t *testing.T) {
	composed := agent.ComposeInstructions("custom ego")
	assert.True(t, strings.HasPrefix(composed, "custom ego"))
	assert.Contains(t, composed, agent.Contract)
	assert.Contains(t, agent.ComposeInstructions(""), agent.DefaultEgo)
}

func TestContract_SourceInspectionAndUnsupportedAnswer(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "memory_inspect")
	assert.Contains(t, contract, "exact original wording")
	assert.Contains(t, contract, "A candidate is a lead")
	assert.Contains(t, contract, "If no source supports an answer, say so")
	assert.Contains(t, contract, "explicit\ncorrection")
}

func TestContract_TerminalReplyKeepsLegacyFieldInactive(t *testing.T) {
	contract := agent.Contract
	require.Contains(t, contract, "memory_write")
	assert.Contains(t, contract, "memory_ops")
	assert.Contains(t, contract, "does not apply addressed operations")
	assert.Contains(t, contract, "Pass an empty list")
}

func TestContract_ToolAndFailureGuidanceRemain(t *testing.T) {
	contract := agent.Contract
	for _, name := range []string{"file_read", "file_write", "file_list", "message_send", "shell_exec"} {
		assert.Contains(t, contract, name)
	}
	assert.Contains(t, contract, "switch tools")
	assert.Contains(t, contract, "Never fabricate the fact")
}

package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
)

func TestComposeInstructionsKeepsThePlatformContract(t *testing.T) {
	composed := agent.ComposeInstructions("custom ego", "")
	assert.True(t, strings.HasPrefix(composed, "custom ego"))
	assert.Contains(t, composed, agent.Contract)
	assert.Contains(t, agent.ComposeInstructions("", ""), agent.DefaultEgo)
}

// TestComposeInstructions_EmptyOperatorInstructionsChangeNothing holds a
// deployment that names no instructions file to the instructions it had
// before the file existed, byte for byte: the trimmed ego, one blank line,
// the Contract (ADR 0024). The expected strings are that composition
// written out, not a call to the function under test.
func TestComposeInstructions_EmptyOperatorInstructionsChangeNothing(t *testing.T) {
	tests := []struct {
		name     string
		ego      string
		operator string
		want     string
	}{
		{"default ego", "", "", strings.TrimSpace(agent.DefaultEgo) + "\n\n" + agent.Contract},
		{"custom ego", "  custom ego\n", "", "custom ego\n\n" + agent.Contract},
		{"blank operator instructions", "custom ego", " \n\t\n", "custom ego\n\n" + agent.Contract},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, agent.ComposeInstructions(tt.ego, tt.operator))
		})
	}
}

// TestComposeInstructions_OperatorInstructionsSitBetweenEgoAndContract
// holds the order ADR 0024 fixes: operator instructions do not displace
// DefaultEgo, and the Contract stays last.
func TestComposeInstructions_OperatorInstructionsSitBetweenEgoAndContract(t *testing.T) {
	const operator = "Reply to each envelope through message_send."

	t.Run("empty ego keeps DefaultEgo", func(t *testing.T) {
		want := strings.TrimSpace(agent.DefaultEgo) + "\n\n" + operator + "\n\n" + agent.Contract
		assert.Equal(t, want, agent.ComposeInstructions("", "\n"+operator+"\n"))
	})
	t.Run("custom ego", func(t *testing.T) {
		want := "custom ego\n\n" + operator + "\n\n" + agent.Contract
		assert.Equal(t, want, agent.ComposeInstructions("custom ego", operator))
	})
}

func TestContract_SourceInspectionAndUnsupportedAnswer(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "memory_inspect")
	assert.Contains(t, contract, "exact original wording")
	assert.Contains(t, contract, "a lead and not\n  proof")
	assert.Contains(t, contract, "If no source\n  supports an answer, say so")
	assert.Contains(t, contract, "explicit correction")
}

func TestContract_TerminalCallCarriesTheReplyAlone(t *testing.T) {
	contract := agent.Contract
	require.Contains(t, contract, "memory_write")
	assert.Contains(t, contract, "`reply`")
	assert.NotContains(t, contract, "`note`")
	assert.NotContains(t, contract, "memory_ops")
}

func TestContract_MemoryFilesAndSkillsAreTheAgentsToWrite(t *testing.T) {
	contract := agent.Contract
	for _, name := range []string{"memory_file_write", "USER.md", "MEMORY.md", "skill_write", "skill_view"} {
		assert.Contains(t, contract, name)
	}
	assert.NotContains(t, contract, "Recalled memories")
}

func TestContract_SearchResolvesReferences(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "memory_search")
	assert.Contains(t, contract, "standalone query")
}

func TestContract_ToolAndFailureGuidanceRemain(t *testing.T) {
	contract := agent.Contract
	for _, name := range []string{"file_read", "file_write", "file_list", "message_send", "shell_exec"} {
		assert.Contains(t, contract, name)
	}
	assert.Contains(t, contract, "switch tools")
	assert.Contains(t, contract, "Never fabricate the fact")
}

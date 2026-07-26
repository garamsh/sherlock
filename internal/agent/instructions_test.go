package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/gagent/internal/agent"
)

func TestComposeInstructions_EmbeddedDefaultPersona(t *testing.T) {
	composed := agent.ComposeInstructions("")

	assert.True(t, strings.HasPrefix(composed, strings.TrimSpace(agent.DefaultPersona)),
		"empty persona selects the embedded AGENT.md")
	assert.Contains(t, composed, agent.Contract)
	assert.Contains(t, composed, "```memory", "writeback contract is always present")
}

func TestComposeInstructions_CustomPersona(t *testing.T) {
	composed := agent.ComposeInstructions("You are a pirate.")

	assert.True(t, strings.HasPrefix(composed, "You are a pirate."))
	assert.NotContains(t, composed, strings.TrimSpace(agent.DefaultPersona),
		"a custom persona replaces the embedded default")
	assert.Contains(t, composed, agent.Contract,
		"the platform contract is appended regardless of the persona source")
}

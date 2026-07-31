package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
)

func TestComposeInstructions_EmbeddedDefaultEgo(t *testing.T) {
	composed := agent.ComposeInstructions("")

	assert.True(t, strings.HasPrefix(composed, strings.TrimSpace(agent.DefaultEgo)),
		"empty ego selects the embedded EGO.md")
	assert.Contains(t, composed, agent.Contract)
	assert.Contains(t, composed, "```memory", "writeback contract is always present")
}

func TestComposeInstructions_CustomEgo(t *testing.T) {
	composed := agent.ComposeInstructions("You are a pirate.")

	assert.True(t, strings.HasPrefix(composed, "You are a pirate."))
	assert.NotContains(t, composed, strings.TrimSpace(agent.DefaultEgo),
		"a custom ego replaces the embedded default")
	assert.Contains(t, composed, agent.Contract,
		"the platform contract is appended regardless of the ego source")
}

// TestContract_ExampleMemoryBlock pins the literal ```memory ... ```
// shape the contract teaches the model (issue #96; proposal #94). The
// example appears verbatim so the model can copy the JSON shape without
// guessing.
func TestContract_ExampleMemoryBlock(t *testing.T) {
	const example = `{"keep":["m1"],"add":[{"content":"user prefers dark mode","type":"semantic"}]}`

	intro := "A correct block looks like:"
	assert.Contains(t, agent.Contract, intro,
		"the example is introduced as a shape reference")
	assert.Contains(t, agent.Contract, example,
		"the example JSON block is present verbatim")
	assert.Greater(t, strings.Index(agent.Contract, example),
		strings.Index(agent.Contract, intro),
		"the example JSON follows its introduction")
}

// TestContract_ToolSelectionGuidance asserts each tool carries a
// selection sentence ("use this for X") so the model can pick the right
// tool on the first attempt (issue #96; proposal #94; ADR 0012).
func TestContract_ToolSelectionGuidance(t *testing.T) {
	cases := []struct {
		tool     string
		fragment string
	}{
		{"memory", "search and read your long-term memory"},
		{"files", "read and write files in your workspace"},
		{"message_send", "reply through message_send"},
		{"shell_exec", "run a shell command on the workspace"},
	}
	for _, tc := range cases {
		assert.Contains(t, agent.Contract, tc.fragment,
			"%s tool has selection guidance", tc.tool)
	}
}

// TestContract_ShellExecSingleStringCommand pins the QA #103 follow-on
// for ADR 0012: the shell_exec `command` field is a single string
// passed to sh -c, and the contract makes this explicit so the model
// does not pass pre-split argument arrays.
func TestContract_ShellExecSingleStringCommand(t *testing.T) {
	assert.Contains(t, agent.Contract, "single string",
		"the contract describes command as a single string")
	assert.Contains(t, agent.Contract, "sh -c",
		"the contract names the shell it is passed to")
}

// TestContract_FailureRecoverySentence asserts the contract tells the
// model to change approach after a tool error and not to repeat the same
// call (issue #96; proposal #94).
func TestContract_FailureRecoverySentence(t *testing.T) {
	assert.Contains(t, agent.Contract,
		"If a tool returns an error, change your approach",
		"the failure-recovery sentence is present")
	assert.Contains(t, agent.Contract,
		"Do not repeat the same call",
		"the no-repeat rule is present")
	assert.Contains(t, agent.Contract,
		"the platform will substitute a synthetic error after two retries",
		"the contract references the platform's repeat short-circuit")
}

// TestContract_StoreVsWritebackAddDistinction pins the QA #103 fix: the
// contract now distinguishes the memory tool's `store` op (which requires
// importance_score and source) from the writeback `add` op (which
// auto-fills them). The model confuses the two without the explicit
// contrast, so the contract must keep the two interfaces side by side.
func TestContract_StoreVsWritebackAddDistinction(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "## Memory: tool call vs writeback",
		"the contract carries a tool-call vs writeback section header")
	assert.Contains(t, contract, "op: store",
		"the contract shows the store op shape")
	assert.Contains(t, contract, "importance_score",
		"the contract mentions importance_score")
	assert.Contains(t, contract, "source",
		"the contract mentions source")
	assert.Contains(t, contract, "auto-filled",
		"the contract names the writeback auto-fill behaviour")
	assert.Contains(t, contract, "`add`",
		"the contract references the writeback add op")
	// The store op and the writeback add op must appear in the same
	// section so the model sees the contrast in one place.
	storeIdx := strings.Index(contract, "op: store")
	addIdx := strings.Index(contract, "`add`")
	require.Greater(t, storeIdx, -1, "store op reference missing")
	require.Greater(t, addIdx, -1, "add op reference missing")
	toolSection := strings.Index(contract, "## Memory: tool call vs writeback")
	writebackSection := strings.Index(contract, "## Memory writeback")
	assert.Greater(t, storeIdx, toolSection)
	assert.Less(t, storeIdx, writebackSection,
		"the store add contrast lives before the writeback section header")
	assert.Greater(t, addIdx, toolSection)
	assert.Less(t, addIdx, writebackSection,
		"the writeback add reference lives in the same contrast section")
}

// TestDefaultEgo_BehaviorGuidance pins the identity + behavior lines the
// expanded EGO.md carries (issue #96; proposal #94).
func TestDefaultEgo_BehaviorGuidance(t *testing.T) {
	assert.Contains(t, agent.DefaultEgo,
		"one continuous session per environment",
		"identity line describes the rolling-session design")
	assert.Contains(t, agent.DefaultEgo,
		"long-term memory across turns",
		"behavior line mentions memory maintenance")
	assert.Contains(t, agent.DefaultEgo,
		"smallest change that solves the task",
		"behavior line prefers the smallest change")
	assert.Contains(t, agent.DefaultEgo,
		"ask for confirmation before",
		"behavior line asks for confirmation before destructive ops")
}

// TestContract_ToolUnavailableFallbackSection pins the new "When a tool
// is unavailable" section (issue #113, QA #108 follow-up). The four
// previously-failing scenarios in QA #108 share a single cause: when
// shell_exec returns an error, the model kept retrying different shell
// commands instead of switching to typed tools. The contract must carry
// the typed-tool alternative map and the "do not retry shell_exec after
// shell_exec has already failed" rule.
func TestContract_ToolUnavailableFallbackSection(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "## When a tool is unavailable",
		"the contract carries the new fallback section header")
	// The five typed-tool alternatives must each be named.
	for _, frag := range []string{
		"`files` (operation: list)",
		"`files` (operation: read)",
		"`files` (operation: write)",
		"`message_send`",
		"answer from context when simple",
	} {
		assert.Contains(t, contract, frag,
			"fallback map carries %q", frag)
	}
	// The no-retry rule must be present.
	assert.Contains(t, contract,
		"Retrying `shell_exec` after `shell_exec` already failed is not",
		"the contract tells the model not to retry shell_exec after a failure")
	// The section must sit between "Memory writeback" and "Replies and
	// failures" so the model reads it in order.
	mwIdx := strings.Index(contract, "## Memory writeback")
	rfIdx := strings.Index(contract, "## Replies and failures")
	tuIdx := strings.Index(contract, "## When a tool is unavailable")
	require.Greater(t, mwIdx, -1, "Memory writeback section missing")
	require.Greater(t, rfIdx, -1, "Replies and failures section missing")
	require.Greater(t, tuIdx, -1, "When a tool is unavailable section missing")
	assert.Greater(t, tuIdx, mwIdx, "fallback section comes after Memory writeback")
	assert.Less(t, tuIdx, rfIdx, "fallback section comes before Replies and failures")
}

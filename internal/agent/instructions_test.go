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

// TestContract_AddRequiredForSharedFacts pins the issue #193 contract
// fix: gpt-4o-mini ended every response with an empty ```memory block
// because the contract made add an opt-in. The contract must state when
// an add is required — a shared fact or preference, a correction, or a
// remember request — and show one example block for a stated fact.
func TestContract_AddRequiredForSharedFacts(t *testing.T) {
	contract := agent.Contract
	const rule = "An `add` op is required"
	idx := strings.Index(contract, rule)
	require.Greater(t, idx, -1, "the add-required rule is missing")
	for _, trigger := range []string{
		"shares a fact or preference",
		"corrects an earlier fact",
		"asks to\nremember",
	} {
		assert.Contains(t, contract, trigger,
			"the add-required rule covers %q", trigger)
	}
	const example = `{"add":[{"content":"user's favorite telescope is the Kepler-9 refractor","type":"semantic"}]}`
	assert.Contains(t, contract, example,
		"a one-example block shows the add for a stated fact")
	assert.Greater(t, strings.Index(contract, example), idx,
		"the example block follows the rule")
	// The rule lives in the Memory writeback section so the model reads
	// it next to the block format it constrains.
	mwIdx := strings.Index(contract, "## Memory writeback")
	tuIdx := strings.Index(contract, "## When a tool is unavailable")
	require.Greater(t, mwIdx, -1, "Memory writeback section missing")
	require.Greater(t, tuIdx, -1, "When a tool is unavailable section missing")
	assert.Greater(t, idx, mwIdx, "the rule comes after the Memory writeback header")
	assert.Less(t, idx, tuIdx, "the rule stays inside the Memory writeback section")
}

// TestContract_ToolSelectionGuidance asserts each tool carries a
// selection sentence ("use this for X") so the model can pick the right
// tool on the first attempt (issue #96; proposal #94; ADR 0008).
func TestContract_ToolSelectionGuidance(t *testing.T) {
	cases := []struct {
		tool     string
		fragment string
	}{
		{"files", "read, write, and list files in your workspace"},
		{"message_send", "reply through message_send"},
		{"shell_exec", "Running a command"},
	}
	for _, tc := range cases {
		assert.Contains(t, agent.Contract, tc.fragment,
			"%s tool has selection guidance", tc.tool)
	}
}

// TestContract_ShellExecIsATool pins issue #279: process exec is the
// workspace's capability, surfaced to the model as the `shell_exec`
// tool (ADR 0008, ADR 0018). The stale "not a tool / never invoke"
// wording is gone; the contract names `shell_exec` in the switch-tools
// fallback map (alongside `files` and `message_send`) and points at
// the workspace Exec RPC so the model knows exec lives behind that
// wire, not as a native capability in the agent process.
func TestContract_ShellExecIsATool(t *testing.T) {
	contract := agent.Contract
	assert.NotContains(t, contract, "Process exec is not a tool",
		"the contract no longer says process exec is not a tool")
	assert.NotContains(t, contract, "execs natively",
		"the contract no longer claims the agent execs natively")
	assert.NotContains(t, contract, "never invoke it directly",
		"the contract no longer tells the model never to invoke exec")
	assert.Contains(t, contract, "shell_exec",
		"the contract names the shell_exec tool")
	assert.Contains(t, contract, "workspace Exec RPC",
		"the contract traces shell_exec to the workspace Exec RPC (ADR 0008)")
	// The switch-tools fallback map covers shell_exec alongside files
	// and message_send so the model has typed guidance for "Running a
	// command" when shell_exec itself fails.
	tuIdx := strings.Index(contract, "## When a tool is unavailable")
	rfIdx := strings.Index(contract, "## Replies and failures")
	require.Greater(t, tuIdx, -1, "When a tool is unavailable section missing")
	require.Greater(t, rfIdx, -1, "Replies and failures section missing")
	frag := "Running a command"
	idx := strings.Index(contract, frag)
	require.Greater(t, idx, -1,
		"shell_exec has no selection guidance in the fallback map")
	assert.Greater(t, idx, tuIdx,
		"shell_exec selection guidance lives in the fallback section")
	assert.Less(t, idx, rfIdx,
		"shell_exec selection guidance stays before Replies and failures")
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

// TestContract_MemoryIsNeverATool pins ADR 0018: the contract advertises
// no memory tool — recall injection covers reads and the writeback block
// is the single write path. The writeback add op keeps its auto-filled
// fields, so the contract never asks the model for importance_score or
// source.
func TestContract_MemoryIsNeverATool(t *testing.T) {
	contract := agent.Contract
	assert.NotContains(t, contract, "- memory:",
		"the contract advertises no memory tool")
	assert.NotContains(t, contract, "op: store",
		"the contract no longer teaches a memory store op")
	assert.NotContains(t, contract, "importance_score",
		"the contract never asks the model for importance_score")
}

// TestContract_RecordOnlyObtainedFacts pins the ADR 0011 contract
// collapse: the #137/#144 no-fabrication rules shrink to one line —
// record only facts actually obtained this turn. The single line forbids
// placeholders from failed tool calls and requires the visible reply to
// match the writeback side effect.
func TestContract_RecordOnlyObtainedFacts(t *testing.T) {
	contract := agent.Contract
	const rule = "Record only facts you actually obtained this turn"
	idx := strings.Index(contract, rule)
	require.Greater(t, idx, -1, "the record-only-obtained-facts rule is missing")
	line := contract[idx:]
	if nl := strings.Index(line, "\n"); nl >= 0 {
		line = line[:nl]
	}
	assert.Contains(t, line, "placeholder",
		"the one-line rule forbids placeholders from failed tool calls")
	assert.Contains(t, line, "visible reply",
		"the one-line rule ties the visible reply to the writeback side effect")
	// The rule lives in the Memory writeback section so the model reads it
	// next to the add-op format it constrains.
	mwIdx := strings.Index(contract, "## Memory writeback")
	tuIdx := strings.Index(contract, "## When a tool is unavailable")
	require.Greater(t, mwIdx, -1, "Memory writeback section missing")
	require.Greater(t, tuIdx, -1, "When a tool is unavailable section missing")
	assert.Greater(t, idx, mwIdx, "the rule comes after the Memory writeback header")
	assert.Less(t, idx, tuIdx, "the rule stays inside the Memory writeback section")
}

// TestContract_AutoCaptureStatement pins the ADR 0012 contract collapse:
// the platform remembers every user message automatically, so the
// remember-request rule and its #164 store-vs-mention / store-vs-file
// clauses shrink to one line — the writeback block carries distilled
// facts, not transcripts.
func TestContract_AutoCaptureStatement(t *testing.T) {
	contract := agent.Contract
	const rule = "Every user message is remembered automatically by the platform"
	idx := strings.Index(contract, rule)
	require.Greater(t, idx, -1, "the auto-capture statement is missing")
	line := contract[idx:]
	if nl := strings.Index(line, "\n"); nl >= 0 {
		line = line[:nl]
	}
	assert.Contains(t, line, "distilled facts, not transcripts",
		"the one-line statement scopes the writeback to distilled facts")
	assert.NotContains(t, contract, "When the user asks you to remember something",
		"the remember-request rule is gone — capture is platform-owned")
	assert.NotContains(t, contract, "the only act of remembering",
		"the store-vs-mention clause is gone")
	assert.NotContains(t, contract, "Writing the fact to a workspace file is not remembering",
		"the store-vs-file clause is gone")
	// The statement lives in the Memory writeback section so the model
	// reads it next to the block format it constrains.
	mwIdx := strings.Index(contract, "## Memory writeback")
	tuIdx := strings.Index(contract, "## When a tool is unavailable")
	require.Greater(t, mwIdx, -1, "Memory writeback section missing")
	require.Greater(t, tuIdx, -1, "When a tool is unavailable section missing")
	assert.Greater(t, idx, mwIdx, "the statement comes after the Memory writeback header")
	assert.Less(t, idx, tuIdx, "the statement stays inside the Memory writeback section")
}

// TestContract_MultiMessageTurn pins ADR 0016: the contract tells the
// model a turn may carry several messages (each with ID and sender,
// replies addressed per message via message_send) and that a later
// message may retract an earlier one — the supersede mechanism.
func TestContract_MultiMessageTurn(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "A turn may carry several messages",
		"the multi-message statement is present")
	assert.Contains(t, contract, "an ID and a sender",
		"each message arrives with an ID and a sender")
	assert.Contains(t, contract, "address replies per message through message_send",
		"replies are addressed per message")
	assert.Contains(t, contract, "retract an earlier one",
		"the retraction statement is present")
	assert.Contains(t, contract, "do not start the retracted work",
		"a retracted request is never started")
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
// shell_exec returned an error, the model kept retrying different
// shell commands instead of switching to typed tools. With shell_exec
// gone, the contract carries the typed-tool alternative map and the
// "do not retry the same tool after it has already failed" rule.
func TestContract_ToolUnavailableFallbackSection(t *testing.T) {
	contract := agent.Contract
	assert.Contains(t, contract, "## When a tool is unavailable",
		"the contract carries the fallback section header")
	// The four typed-tool alternatives must each be named.
	for _, frag := range []string{
		"`files` (operation: list)",
		"`files` (operation: read)",
		"`files` (operation: write)",
		"`message_send`",
	} {
		assert.Contains(t, contract, frag,
			"fallback map carries %q", frag)
	}
	// The no-retry rule must be present.
	assert.Contains(t, contract,
		"Retrying a tool after it has already failed is not",
		"the contract tells the model not to retry a tool after a failure")
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

package memory_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	agentmemory "github.com/garamsh/gagent/internal/agent/memory"
)

// history returns n user messages with distinguishable content.
func history(n int) []agent.Message {
	out := make([]agent.Message, 0, n)
	for i := range n {
		out = append(out, agent.Message{Role: agent.RoleUser, Content: string(rune('a' + i%26))})
	}
	return out
}

// TestCompact_KeepsTheNewestAndDropsTheRest is what compaction does now:
// the oldest turns leave the window and nothing is written anywhere.
//
// It used to summarize them into an episodic memory. #582 removed that
// write — an addressed store holds an address and a value, and a summary
// of a conversation answers no question a read can ask for — and the
// compactor kept the half that bounds the window.
func TestCompact_KeepsTheNewestAndDropsTheRest(t *testing.T) {
	compactor := agentmemory.Compactor{MaxHistoryMessages: 2}

	kept, err := compactor.Compact(context.Background(), history(6))

	require.NoError(t, err)
	assert.Len(t, kept, 2, "the newest messages stay in the window")
	assert.Equal(t, history(6)[4:], kept, "and they are the newest ones")
}

func TestCompact_BelowTheCapKeepsEverything(t *testing.T) {
	compactor := agentmemory.Compactor{MaxHistoryMessages: 10}

	kept, err := compactor.Compact(context.Background(), history(6))

	require.NoError(t, err)
	assert.Len(t, kept, 6)
}

// A zero cap disables compaction: the session keeps its whole history.
func TestCompact_ZeroCapKeepsEverything(t *testing.T) {
	compactor := agentmemory.Compactor{}

	kept, err := compactor.Compact(context.Background(), history(6))

	require.NoError(t, err)
	assert.Len(t, kept, 6)
}

// A tool message answers the assistant message that called for it, so a
// cut landing on one moves past it rather than orphaning it.
func TestCompact_DoesNotLeaveAToolMessageWithoutItsCall(t *testing.T) {
	h := []agent.Message{
		{Role: agent.RoleUser, Content: "a"},
		{Role: agent.RoleAssistant, Content: "b"},
		{Role: agent.RoleTool, Content: "c"},
		{Role: agent.RoleTool, Content: "d"},
		{Role: agent.RoleUser, Content: "e"},
	}
	compactor := agentmemory.Compactor{MaxHistoryMessages: 3}

	kept, err := compactor.Compact(context.Background(), h)

	require.NoError(t, err)
	assert.Equal(t, []agent.Message{{Role: agent.RoleUser, Content: "e"}}, kept,
		"the cut moves past the tool messages it would have orphaned")
}

// TestCompact_KeepsTheLeadingSystemMessage pins what compaction owes the
// session's first system message, which is the composed ego and platform
// contract rather than history: it stays at the head whatever the window
// drops. The session injects it only into an empty history, so a
// compactor that dropped it left the model without the instruction the
// writeback call depends on for the rest of the session (#642).
func TestCompact_KeepsTheLeadingSystemMessage(t *testing.T) {
	instructions := agent.Message{Role: agent.RoleSystem, Content: "instructions"}
	h := append([]agent.Message{instructions}, history(4)...)
	compactor := agentmemory.Compactor{MaxHistoryMessages: 2}

	kept, err := compactor.Compact(context.Background(), h)

	require.NoError(t, err)
	assert.Equal(t, instructions, kept[0], "the instructions lead after a compaction")
	assert.Equal(t, history(4)[2:], kept[1:], "the window still drops the oldest conversation")
}

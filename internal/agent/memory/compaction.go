package memory

import (
	"context"

	"github.com/garamsh/gagent/internal/agent"
)

// DefaultMaxHistoryMessages caps a rolling session's context before the
// oldest messages are dropped.
const DefaultMaxHistoryMessages = 128

// Compactor keeps the newest messages of a rolling session and drops the
// rest (ADR 0006).
//
// It writes nothing. The step used to summarize the dropped turns into an
// episodic memory, and an addressed store has nowhere to put a sentence
// it cannot address (ADR 0018): the store holds an address and a value,
// and a summary of a conversation answers no question a read can ask for.
// On the configuration this repository ships the write already never
// happened — it was refused unless a model generated the summary, and the
// deterministic stand-in does not (#549) — so what an operator running
// that configuration keeps is unchanged.
type Compactor struct {
	MaxHistoryMessages int
}

// Compact implements agent.Compactor.
func (c Compactor) Compact(_ context.Context, history []agent.Message) ([]agent.Message, error) {
	if len(history) > 0 && history[0].Role == agent.RoleSystem {
		history = history[1:]
	}
	keep := c.MaxHistoryMessages
	if keep <= 0 || len(history) <= keep {
		return append([]agent.Message(nil), history...), nil
	}
	// A tool message answers the assistant message that called for it, so
	// the cut moves past any tool message it would have orphaned.
	cut := len(history) - keep
	for cut < len(history) && history[cut].Role == agent.RoleTool {
		cut++
	}
	return append([]agent.Message(nil), history[cut:]...), nil
}

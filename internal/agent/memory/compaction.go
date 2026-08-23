package memory

import (
	"context"

	"github.com/garamsh/gagent/internal/agent"
)

// DefaultMaxHistoryMessages bounds the conversation window a rolling
// session keeps before the oldest messages are dropped. The leading
// system message is retained in addition to it.
const DefaultMaxHistoryMessages = 128

// Compactor keeps the leading system message and the newest messages of
// a rolling session, and drops the rest (ADR 0006).
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

// Compact implements agent.Compactor. The leading system message is the
// session's instructions — the ego and platform contract, not
// conversation — so it stays at the head whatever the window drops; the
// session injects it only into an empty history and cannot restore it.
func (c Compactor) Compact(_ context.Context, history []agent.Message) ([]agent.Message, error) {
	var head []agent.Message
	if len(history) > 0 && history[0].Role == agent.RoleSystem {
		head = history[:1]
		history = history[1:]
	}
	keep := c.MaxHistoryMessages
	if keep > 0 && len(history) > keep {
		// A tool message answers the assistant message that called for
		// it, so the cut moves past any tool message it would orphan.
		cut := len(history) - keep
		for cut < len(history) && history[cut].Role == agent.RoleTool {
			cut++
		}
		history = history[cut:]
	}
	out := make([]agent.Message, 0, len(head)+len(history))
	return append(append(out, head...), history...), nil
}

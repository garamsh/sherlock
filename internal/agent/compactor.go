package agent

import (
	"context"
)

// Compactor is the context-overflow hook point (ADR 0006): when history
// grows past Options.MaxHistoryMessages, the session asks the compactor to
// shrink it. internal/agent/memory implements it; nil keeps history
// unbounded.
type Compactor interface {
	Compact(ctx context.Context, history []Message) ([]Message, error)
}

package agent

import (
	"context"
)

// Recaller is the recall hook point of the memory lifecycle (ADR 0006).
// internal/agent/memory implements it over the memory service and the
// runtime wires it; nil means no recall.
type Recaller interface {
	Recall(ctx context.Context, task string) (Recalled, error)
}

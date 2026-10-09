package gateway

import (
	"context"

	"github.com/garamsh/sherlock/internal/agent"
)

// RuntimeFactory builds the agent's runtime. The events sink is the
// agent's monitoring stream and outlives any single runtime.
type RuntimeFactory interface {
	New(ctx context.Context, agentID string, events agent.EventSink) (*Runtime, error)
}

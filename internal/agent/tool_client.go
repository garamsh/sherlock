package agent

import (
	"context"
	"encoding/json"

	"github.com/garamsh/sherlock/internal/tool"
)

// ToolClient is the platform's tool client surface the session consumes
// (ADR 0017 §Tool contract). Production uses *tool.Registry; tests may
// substitute any implementation that exposes the same listing and call
// surface.
type ToolClient interface {
	// List returns the descriptors (sorted by name) the model sees.
	List() []tool.Descriptor
	// Call dispatches one tool call. Validation and permission policy
	// are the client's responsibility, and so is holding a
	// non-conforming output back: a call whose output fails its schema
	// returns an error and the output does not reach the model.
	Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)
}

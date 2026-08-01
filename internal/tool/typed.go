package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

// CallTyped handles the JSON boundary of a tool's Call method: it decodes
// input into In, runs the typed function fn, and marshals fn's result.
// Tools delegate to it as `return tool.CallTyped(ctx, t.doCall, input)`
// so the typed struct stays the canonical shape and the parse/marshal
// boilerplate lives in one place.
func CallTyped[In any, Out any](
	ctx context.Context,
	fn func(context.Context, In) (Out, error),
	input json.RawMessage,
) (json.RawMessage, error) {
	var in In
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	out, err := fn(ctx, in)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

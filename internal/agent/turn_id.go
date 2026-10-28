package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// TurnIDHeader is the request header a chat-model client sends the
// running turn's identifier in, so an endpoint can tell the requests of
// one turn from those of another (ADR 0030).
const TurnIDHeader = "X-Turn-Id"

type turnIDKey struct{}

// WithTurnID carries a turn's identifier to every model request made
// under ctx (TurnIDHeader).
func WithTurnID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, turnIDKey{}, id)
}

// TurnID returns the identifier of the turn ctx runs in, or "" outside a
// turn.
func TurnID(ctx context.Context) string {
	id, _ := ctx.Value(turnIDKey{}).(string)
	return id
}

// mintTurnID returns a fresh turn identifier: 128 random bits as 32
// lowercase hex characters.
func mintTurnID() string {
	b := make([]byte, 16)
	// crypto/rand.Read never returns an error; it crashes the program
	// when the system's randomness source fails.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

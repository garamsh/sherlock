package agent

import "context"

// SourceMessage is visible conversation evidence. Runtime instructions, tool
// output, and model reasoning have no representation in this type.
type SourceMessage struct {
	ID         int64
	ExternalID string
	Role       Role
	Content    string
}

// ConversationMemory persists visible turns and links retrieval to evidence.
type ConversationMemory interface {
	RecordTurn(context.Context, []SourceMessage) error
	Recent(context.Context, int) ([]SourceMessage, error)
	Search(context.Context, []string, int) ([]ConversationHit, error)
	Short(context.Context, int64) (ShortMemory, error)
	Source(context.Context, int64) (SourceMessage, error)
}

type ConversationHit struct {
	ID      int64
	Summary string
	ShortID int64
}

type ShortMemory struct {
	ID       int64
	Summary  string
	SourceID int64
}

type inboundSourceKey struct{}

// WithInboundSources carries the gateway's drained message IDs and original
// contents through one run. A one-shot run without an envelope uses task text.
func WithInboundSources(ctx context.Context, sources []SourceMessage) context.Context {
	return context.WithValue(ctx, inboundSourceKey{}, append([]SourceMessage(nil), sources...))
}

func inboundSources(ctx context.Context, task string) []SourceMessage {
	if sources, ok := ctx.Value(inboundSourceKey{}).([]SourceMessage); ok && len(sources) > 0 {
		return sources
	}
	return []SourceMessage{{Role: RoleUser, Content: task}}
}

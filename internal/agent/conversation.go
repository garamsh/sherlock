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
	RecordTurn(context.Context, []SourceMessage, ConversationSummarizer) error
	Recent(context.Context, int) ([]SourceMessage, error)
	Search(context.Context, string, int) ([]ConversationHit, error)
	PendingSources(context.Context) ([]SourceMessage, error)
	RecentMid(context.Context, int) ([]ConversationHit, error)
	Source(context.Context, int64) (SourceMessage, error)
	// Note returns the saved note, empty when none has been written.
	Note(context.Context) (string, error)
	// SetNote replaces the saved note.
	SetNote(context.Context, string) error
}

// ConversationSummarizer synthesizes one bounded episode from new messages.
type ConversationSummarizer interface {
	Summarize(context.Context, []string) (string, error)
}

// Embedder turns text into a vector whose cosine with another of the same
// model's vectors measures their similarity.
type Embedder interface {
	Embed(context.Context, string) ([]float32, error)
}

// ConversationHit is an immutable mid episode linked directly to its source interval.
type ConversationHit struct {
	ID          int64
	Summary     string
	SourceFirst int64
	SourceLast  int64
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

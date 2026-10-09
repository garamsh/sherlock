package agent

import (
	"context"
)

// ConversationMemory persists visible turns and links retrieval to evidence.
type ConversationMemory interface {
	RecordTurn(context.Context, []SourceMessage, ConversationSummarizer) error
	Recent(context.Context, int) ([]SourceMessage, error)
	Search(context.Context, string, int) ([]ConversationHit, error)
	PendingSources(context.Context) ([]SourceMessage, error)
	RecentMid(context.Context, int) ([]ConversationHit, error)
	// Linked ranks the mid episodes the entity walk reaches from the
	// entities the query names.
	Linked(context.Context, string) (Linked, error)
	Source(context.Context, int64) (SourceMessage, error)
	// Note returns the saved note, empty when none has been written.
	Note(context.Context) (string, error)
	// SetNote replaces the saved note.
	SetNote(context.Context, string) error
}

package agent

import (
	"context"
)

// ConversationMemory persists visible turns and links retrieval to evidence.
type ConversationMemory interface {
	RecordTurn(context.Context, []SourceMessage, ConversationSummarizer) error
	Recent(context.Context, int) ([]SourceMessage, error)
	Search(context.Context, string, int) ([]ConversationHit, error)
	// Linked ranks the mid episodes the entity walk reaches from the
	// entities the query names.
	Linked(context.Context, string) (Linked, error)
	Source(context.Context, int64) (SourceMessage, error)
	// AgentFiles returns the agent's memory files and skills by path.
	AgentFiles(context.Context) (map[string]string, error)
	// SetAgentFile replaces one of them.
	SetAgentFile(ctx context.Context, path, content string) error
}

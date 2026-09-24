package agent

import (
	"context"
	"strings"
	"unicode"
)

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
	Search(context.Context, []string, int) ([]ConversationHit, error)
	PendingSources(context.Context) ([]SourceMessage, error)
	RecentMid(context.Context, int) ([]ConversationHit, error)
	Source(context.Context, int64) (SourceMessage, error)
}

// ConversationSummarizer synthesizes one bounded episode from new messages.
type ConversationSummarizer interface {
	Summarize(context.Context, []string) (string, error)
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

// SearchTokens supplies the shared lexical policy for phrase fallback and FTS matching.
func SearchTokens(terms []string) []string {
	var tokens []string
	seen := map[string]bool{}
	for _, term := range terms {
		for _, word := range strings.FieldsFunc(strings.ToLower(term), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
			if len([]rune(word)) < 2 || seen[word] || searchStopWords[word] {
				continue
			}
			seen[word] = true
			tokens = append(tokens, word)
		}
	}
	return tokens
}

var searchStopWords = map[string]bool{"the": true, "and": true, "for": true, "was": true, "what": true, "when": true, "where": true, "who": true, "which": true, "this": true, "that": true, "with": true, "from": true, "about": true, "does": true, "did": true, "have": true, "has": true, "are": true, "you": true, "your": true, "user": true, "assistant": true}

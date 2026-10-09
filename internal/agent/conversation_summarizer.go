package agent

import "context"

// ConversationSummarizer synthesizes one bounded episode from new messages.
type ConversationSummarizer interface {
	Summarize(context.Context, []string) (Synthesis, error)
}

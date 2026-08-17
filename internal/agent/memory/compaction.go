package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/memory"
)

const (
	// DefaultMaxHistoryMessages caps a rolling session's context before the
	// oldest messages are compacted into memory.
	DefaultMaxHistoryMessages = 128
	contextOverflowSource     = "context-overflow"
	contextOverflowImportance = 0.5
)

// Compactor summarizes the oldest conversation history into an episodic
// memory and retains the newest messages for the rolling session.
type Compactor struct {
	Svc                memory.Service
	Summarizer         memory.Summarizer
	MaxHistoryMessages int
}

// Compact implements agent.Compactor.
func (c Compactor) Compact(ctx context.Context, history []agent.Message) ([]agent.Message, error) {
	if len(history) > 0 && history[0].Role == agent.RoleSystem {
		history = history[1:]
	}
	keep := c.MaxHistoryMessages
	if keep <= 0 || len(history) <= keep {
		return append([]agent.Message(nil), history...), nil
	}

	cut := len(history) - keep
	for cut < len(history) && history[cut].Role == agent.RoleTool {
		cut++
	}
	contents := make([]string, 0, cut)
	for _, msg := range history[:cut] {
		content := msg.Content
		if msg.Role == agent.RoleSystem {
			if framedTask, ok := strings.CutPrefix(content, "## Current task\n"); ok {
				content, _, _ = strings.Cut(framedTask, "\n\n## Recalled memories\n")
			}
		}
		if content != "" {
			contents = append(contents, content)
		}
	}
	if len(contents) == 0 {
		return append([]agent.Message(nil), history[cut:]...), nil
	}

	summary, err := c.Summarizer.Summarize(ctx, contents)
	if err != nil {
		return nil, fmt.Errorf("summarize history: %w", err)
	}
	// What ADR 0006 licenses this write on is that the content is
	// model-generated, not platform capture. A summarizer that did not
	// summarize returns the evicted turns themselves, so storing it
	// would put a message the model never stated as a fact into the
	// searchable store. The turns still leave the window; only the
	// write is refused.
	if !summary.Generated {
		return append([]agent.Message(nil), history[cut:]...), nil
	}
	if _, err := c.Svc.Store(ctx, memory.StoreInput{
		Content:         summary.Text,
		Type:            memory.TypeEpisodic,
		ImportanceScore: contextOverflowImportance,
		Source:          contextOverflowSource,
	}); err != nil {
		return nil, fmt.Errorf("store history summary: %w", err)
	}
	return append([]agent.Message(nil), history[cut:]...), nil
}

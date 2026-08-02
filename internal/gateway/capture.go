package gateway

import (
	"context"
	"fmt"

	"github.com/garamsh/gagent/internal/memory"
)

// CapturedTurnImportance is the low default importance assigned to
// auto-captured inbound messages, so curated facts (DefaultAddImportance)
// outrank raw turns in recall (ADR 0016).
const CapturedTurnImportance = 0.1

// SourceUserTurn is the source recorded on auto-captured inbound user
// messages (ADR 0016).
const SourceUserTurn = "user-turn"

// Capture stores one inbound user message verbatim as an episodic memory
// with no model involvement (ADR 0016). The platform captures at accept
// time — before the message is queued and its run starts — so a failed
// run or a gateway restart mid-queue never loses the capture.
type Capture struct {
	// Svc is the environment's memory service.
	Svc memory.Service
}

// Capture implements the inbound-message capture step.
func (c Capture) Capture(ctx context.Context, content string) error {
	if _, err := c.Svc.Store(ctx, memory.StoreInput{
		Content:         content,
		Type:            memory.TypeEpisodic,
		ImportanceScore: CapturedTurnImportance,
		Source:          SourceUserTurn,
	}); err != nil {
		return fmt.Errorf("capture inbound message: %w", err)
	}
	return nil
}

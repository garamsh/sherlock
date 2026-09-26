package study

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

// Shipped recall bounds, as internal/agent/memory/conversation.go and
// internal/agent/search.go set them at 03cbdff.
const (
	shippedRecent  = 2
	shippedSimilar = 5
)

// ShippedArm is the shipped structure over a shared store, read the way
// production reads it. S is ShippedArm("S", "", "b4", "b8"); M is the same
// memory with the chaining instruction. Another arm may start from it.
type ShippedArm struct {
	ArmName     string
	Instruction string
	Batches     []string
}

func (a ShippedArm) Name() string         { return a.ArmName }
func (a ShippedArm) Settings() []string   { return a.Batches }
func (a ShippedArm) Instructions() string { return a.Instruction }

func batchOf(setting string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(setting, "b%d", &n); err != nil {
		return 0, fmt.Errorf("setting %q is not a batch such as b4", setting)
	}
	return n, nil
}

// Build copies the shared store at the setting's batch; the shipped
// structure stores one summary and one vector per mid.
func (a ShippedArm) Build(_ context.Context, env *Env, setting, dir string) (Built, error) {
	n, err := batchOf(setting)
	if err != nil {
		return Built{}, err
	}
	shared, err := env.SharedStore(n)
	if err != nil {
		return Built{}, err
	}
	if err := CopyDir(shared, dir); err != nil {
		return Built{}, err
	}
	mids, chars, err := MidStats(filepath.Join(dir, "memory.db"))
	if err != nil {
		return Built{}, err
	}
	return Built{StoredChars: chars, Vectors: mids, Units: map[string]int{"mid": mids}}, nil
}

func (a ShippedArm) Open(_ context.Context, env *Env, _ string, dir string) (Memory, error) {
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	if err != nil {
		return nil, err
	}
	return ShippedMemory{Conversation: store}, nil
}

// ShippedMemory reads a production store as production does.
type ShippedMemory struct {
	Conversation *sqlite.Store
}

// Recall is production's pre-turn recall without its pending previews:
// the two newest mids, then the most similar, deduplicated, at most seven.
func (m ShippedMemory) Recall(ctx context.Context, task string) ([]Item, error) {
	recent, err := m.Conversation.RecentMid(ctx, shippedRecent)
	if err != nil {
		return nil, err
	}
	hits, err := m.Conversation.Search(ctx, task, shippedSimilar+len(recent))
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, shippedSimilar+len(recent))
	seen := map[int64]bool{}
	for i, h := range append(recent, hits...) {
		if seen[h.ID] {
			continue
		}
		seen[h.ID] = true
		kind := "historical"
		if i < len(recent) {
			kind = "recent"
		}
		out = append(out, midItem(h, kind))
		if len(out) == shippedSimilar+len(recent) {
			break
		}
	}
	return out, nil
}

// Search is production's memory_search ranking.
func (m ShippedMemory) Search(ctx context.Context, query string, limit int) ([]Item, error) {
	hits, err := m.Conversation.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(hits))
	for _, h := range hits {
		out = append(out, midItem(h, "historical"))
	}
	return out, nil
}

func (m ShippedMemory) Store() agent.ConversationMemory { return m.Conversation }
func (m ShippedMemory) Close() error                    { return m.Conversation.Close() }

func midItem(h agent.ConversationHit, kind string) Item {
	return Item{ID: h.ID, Level: "mid", Kind: kind, SourceFirst: h.SourceFirst, SourceLast: h.SourceLast, Text: h.Summary}
}

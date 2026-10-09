package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// SourceMessage is visible conversation evidence. Runtime instructions, tool
// output, and model reasoning have no representation in this type.
// ExternalID is unique within (Issuer, Sender): the verified issuer and
// subject of a signed message, or an empty Issuer and the transport
// principal that delivered an unsigned one. Poster is the transport
// principal that posted the message, signed or not (ADR 0026).
type SourceMessage struct {
	ID         int64
	ExternalID string
	Issuer     string
	Sender     string
	Poster     string
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
	// Linked ranks the mid episodes the entity walk reaches from the
	// entities the query names.
	Linked(context.Context, string) (Linked, error)
	Source(context.Context, int64) (SourceMessage, error)
	// Note returns the saved note, empty when none has been written.
	Note(context.Context) (string, error)
	// SetNote replaces the saved note.
	SetNote(context.Context, string) error
}

// ConversationSummarizer synthesizes one bounded episode from new messages.
type ConversationSummarizer interface {
	Summarize(context.Context, []string) (Synthesis, error)
}

// Synthesis is one batch's episode as the session model wrote it: the
// summary and the names of the entities the batch mentions, in the order
// the model wrote them.
type Synthesis struct {
	Summary  string
	Entities []string
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

// Linked is the entity walk's answer for a query: the stored entity names
// the query contains, sorted, and the mid episodes reached from them, best
// first.
type Linked struct {
	Seeds []string
	Hits  []LinkedHit
}

// LinkedHit is a mid episode the walk reached. Its subject is the first
// entity its synthesis named.
type LinkedHit struct {
	ConversationHit
	Subject string
}

// FoundEpisode is a mid episode recall or memory_search delivers, and
// whether the entity walk delivered it.
type FoundEpisode struct {
	ConversationHit
	Linked bool
}

// Interleave fills up to slots episodes from the similar list and the
// walk's list in turn, skipping and then marking the episodes in seen; an
// exhausted list gives its turns to the other (ADR 0019). A walk episode
// whose subject is none of the query's entities opens with whom it is
// about and whom the question names.
func Interleave(slots int, seen map[int64]bool, similar []ConversationHit, linked Linked) []FoundEpisode {
	lists := [][]FoundEpisode{make([]FoundEpisode, len(similar)), make([]FoundEpisode, len(linked.Hits))}
	for i, hit := range similar {
		lists[0][i] = FoundEpisode{ConversationHit: hit}
	}
	for i, hit := range linked.Hits {
		lists[1][i] = FoundEpisode{ConversationHit: hit.ConversationHit, Linked: true}
		if !slices.Contains(linked.Seeds, hit.Subject) {
			lists[1][i].Summary = attribution(hit.Subject, linked.Seeds) + hit.Summary
		}
	}
	var out []FoundEpisode
	for len(out) < slots {
		took := false
		for i := range lists {
			if len(out) == slots {
				break
			}
			episode, ok, rest := unseen(lists[i], seen)
			lists[i] = rest
			if ok {
				seen[episode.ID] = true
				out, took = append(out, episode), true
			}
		}
		if !took {
			break
		}
	}
	return out
}

// unseen pops the list's first episode not in seen and returns the rest
// of the list.
func unseen(list []FoundEpisode, seen map[int64]bool) (FoundEpisode, bool, []FoundEpisode) {
	for len(list) > 0 {
		episode := list[0]
		list = list[1:]
		if !seen[episode.ID] {
			return episode, true, list
		}
	}
	return FoundEpisode{}, false, nil
}

func attribution(subject string, seeds []string) string {
	quoted := make([]string, len(seeds))
	for i, seed := range seeds {
		quoted[i] = fmt.Sprintf("%q", seed)
	}
	return fmt.Sprintf("About %q; the question names %s. ", subject, strings.Join(quoted, " and "))
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

// drainedIDs returns the IDs of the inbound messages the turn drained,
// which every tool the turn calls is handed as the messages it replies
// to. A one-shot run without an envelope drained none.
func drainedIDs(sources []SourceMessage) []string {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.ExternalID != "" {
			ids = append(ids, source.ExternalID)
		}
	}
	return ids
}

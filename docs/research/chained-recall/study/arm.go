// Package study is the shared basis of the #874 chained-recall study
// (docs/research/chained-recall/). It holds the one interface every arm
// implements, the corpus every arm is built from, and the runner that
// builds an arm and records what it delivers.
//
// An arm is a command under arms/<name>/ whose main is
//
//	func main() { study.Main(New()) }
//
// with New returning its Arm. study.Main gives it the subcommands build
// and offline (and, later, live); their outputs land under
// evidence/offline/<arm>/ in the shape score.py reads, so no arm writes a
// scorer of its own and no arm is registered anywhere else.
package study

import (
	"context"

	"github.com/garamsh/sherlock/internal/agent"
)

// Arm is one memory structure under test. Its methods are called in this
// order: Settings once; Build once per setting; Open once per setting for
// the offline stage, and once per probe (on a copy of what Build wrote) for
// the live stage.
type Arm interface {
	// Name is the arm's short name: S, M, L or H.
	Name() string
	// Settings are the compression settings the arm is measured at, such
	// as "b4" and "b8" (batch size) or "g4" and "g8" (group size).
	Settings() []string
	// Build writes the arm's memory of the whole corpus (Env.Corpus: the
	// replication base, then the chain stream) into dir, and reports what
	// it stores. It may copy a shared store (Env.SharedStore) and add to
	// the copy; it never writes to a shared store. Model calls go through
	// Env.Model, so the ledger attributes and bounds them; embeddings go
	// through Env.Embedder, the local text-embeddings-inference server.
	Build(ctx context.Context, env *Env, setting, dir string) (Built, error)
	// Open reads a memory Build wrote into dir. For the live stage dir is
	// a per-probe copy that the probe's turn writes to.
	Open(ctx context.Context, env *Env, setting, dir string) (Memory, error)
	// Instructions is text the live session appends to the default ego;
	// "" for none. It is how an arm changes what the model is told
	// without changing the platform contract.
	Instructions() string
}

// Memory is an opened arm memory.
type Memory interface {
	// Recall is what the arm puts into context before the act loop for a
	// task, most important first. The shipped structure returns its two
	// newest mids and up to five similar ones. Pending source previews are
	// not items: the live adapter adds them from Store, as production does.
	Recall(ctx context.Context, task string) ([]Item, error)
	// Search is what the arm returns for a query the model writes to
	// memory_search, at most limit items unless the arm's design says
	// otherwise (and its README states it).
	Search(ctx context.Context, query string, limit int) ([]Item, error)
	// Store is the production conversation store under the arm. The live
	// session records its turn, restores recent messages, reads sources
	// for memory_inspect and keeps the note through it; its Search is not
	// used, Memory.Search is.
	Store() agent.ConversationMemory
	Close() error
}

// Tracer is implemented by a Memory whose mechanism leaves evidence per
// call, which the arm's control reads: walk steps taken, entities seeded,
// groups delivered. The runner records LastTrace after every Recall and
// Search beside the items.
type Tracer interface {
	LastTrace() map[string]any
}

// Item is one delivered memory. Level names what it is ("mid" for a mid
// episode; an arm names its own other levels, such as "group"). Kind is
// why it was delivered, shown to the model as the recall block's age label
// ("recent" and "historical" in the shipped recall; an arm may add its
// own, such as "linked"). SourceFirst and SourceLast are the inclusive
// interval of source messages the item summarizes, which is what the
// scorer and memory_inspect follow. Text is exactly what the model reads,
// before the study appends the label that names the item and its interval.
type Item struct {
	ID          int64  `json:"id"`
	Level       string `json:"level"`
	Kind        string `json:"kind"`
	SourceFirst int64  `json:"first"`
	SourceLast  int64  `json:"last"`
	Text        string `json:"text"`
}

// Built reports what an arm stores for one setting. StoredChars counts
// every character the arm keeps for retrieval beyond the source archive
// (summaries at every level, entity names, link labels); Vectors counts
// the embeddings it keeps. Units counts the retrievable items. The study
// divides StoredChars by the corpus's source characters.
type Built struct {
	StoredChars int            `json:"stored_chars"`
	Vectors     int            `json:"vectors"`
	Units       map[string]int `json:"units"`
	// Notes is anything the arm's control needs recorded, such as links
	// written or groups synthesized.
	Notes map[string]any `json:"notes,omitempty"`
}

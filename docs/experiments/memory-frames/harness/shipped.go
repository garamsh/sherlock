package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/garamsh/gagent/internal/memory"
	"github.com/garamsh/gagent/internal/memory/sqlite"
)

// dbName is the store file the shipped frame opens inside its directory.
// The WAL and shared-memory files sit beside it and count toward what the
// frame stores.
const dbName = "memory.db"

// shippedFrame is internal/memory behind the seam: the SQLite store, the
// hybrid search, the ranking function and the after-turn lifecycle this
// project ships, called through memory.Service rather than reimplemented.
//
// The write path it cannot supply itself is the writeback's op kind, which
// in production is a model decision (docs/architecture/memory.md: the store
// is curation-only). The frame derives it from the observation the arm
// hands it — a turn carrying an extracted fact is a semantic add, a turn
// carrying none is an episodic add — which is the mapping
// internal/agent/memory/writeback.go applies to an add op, and the two arms then
// bracket what the decider is worth.
type shippedFrame struct {
	dir      string
	embedder memory.Embedder
	store    *sqlite.Store
	svc      memory.Service
	// budget is the tokens a recall injects; zero takes the shipped
	// default, which is what every figure before this round was measured at.
	budget int
	// now is the simulated clock the ranking function's recency term and
	// the retention window both read. Every entry point sets it from the
	// turn before doing anything else, so a decade passes inside the run.
	now time.Time
}

// NewShippedFrame opens a store under dir and wires the shipped service
// over it.
func NewShippedFrame(dir string, counter *EmbedCounter, budget int) (Frame, error) {
	f := &shippedFrame{dir: dir, budget: budget, embedder: counter.Wrap(memory.NewHashingEmbedder())}
	if err := f.open(); err != nil {
		return nil, err
	}
	return f, nil
}

// open wires a store and the shipped service over it.
func (f *shippedFrame) open() error {
	store, err := sqlite.Open(filepath.Join(f.dir, dbName))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	f.store = store
	f.svc = memory.NewService(store, f.embedder, memory.NewConcatSummarizer(),
		memory.WithClock(func() time.Time { return f.now }))
	return nil
}

// Observe stores the turn and then runs the two passes the gateway runs
// after a completed turn (internal/gateway/runner.go): threshold
// consolidation and tiered retention. Leaving them out would measure a
// lifecycle this project does not run.
func (f *shippedFrame) Observe(ctx context.Context, t Turn) error {
	f.now = t.At
	typ, salience := memory.TypeEpisodic, memory.SalienceAddEpisodic
	if len(t.Written) > 0 {
		typ, salience = memory.TypeSemantic, memory.SalienceAddSemantic
	}
	if _, err := f.svc.Store(ctx, memory.StoreInput{
		Content:         t.Text,
		Type:            typ,
		ImportanceScore: salience,
		Source:          "workload",
	}); err != nil {
		return fmt.Errorf("store turn %d: %w", t.Index, err)
	}
	if _, err := f.svc.ConsolidateIfDue(ctx); err != nil {
		return fmt.Errorf("consolidate after turn %d: %w", t.Index, err)
	}
	if _, err := f.svc.Retain(ctx); err != nil {
		return fmt.Errorf("retain after turn %d: %w", t.Index, err)
	}
	return nil
}

// Context returns what a turn's recall would inject: the shipped hybrid
// search on the turn's text, bounded by the shipped token budget.
//
// What it reads is the whole active index. The ranking core scores every
// active memory against the query before any bound applies, so the count
// below is the active count, read again here only to report it.
func (f *shippedFrame) Context(ctx context.Context, t Turn) (Injected, error) {
	f.now = t.At
	recalled, err := f.svc.Recall(ctx, t.Text, f.budget)
	if err != nil {
		return Injected{}, fmt.Errorf("recall at turn %d: %w", t.Index, err)
	}
	active, err := f.store.Active(ctx)
	if err != nil {
		return Injected{}, fmt.Errorf("list active at turn %d: %w", t.Index, err)
	}
	contents := make([]string, 0, len(recalled))
	for _, m := range recalled {
		contents = append(contents, m.Content)
	}
	return Injected{Text: strings.Join(contents, "\n"), Scanned: len(active)}, nil
}

// Size counts the active index, which is what a query reaches; the archive
// holds what the lifecycle retired and searches never see it. Stored bytes
// are both of them plus the indexes, which is what a decade of turns costs
// on disk.
//
// The store is closed before the file is measured and opened again after.
// SQLite writes ahead into a second file and folds it into the database at
// a checkpoint, so measuring while it is open reports several megabytes of
// buffer as though they were stored data, and skipping that file leaves out
// what has not been folded in yet. Closing does the fold.
func (f *shippedFrame) Size(ctx context.Context) (Size, error) {
	active, err := f.store.Active(ctx)
	if err != nil {
		return Size{}, fmt.Errorf("list active: %w", err)
	}
	out := Size{Units: len(active)}
	for _, m := range active {
		out.ContentBytes += int64(len(m.Content))
	}
	if err := f.store.Close(); err != nil {
		return Size{}, fmt.Errorf("close store: %w", err)
	}
	out.StoredBytes, err = storedBytes(f.dir)
	if err != nil {
		return Size{}, err
	}
	if err := f.open(); err != nil {
		return Size{}, err
	}
	return out, nil
}

// storedBytes is every file the store keeps under dir.
func storedBytes(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", dir, err)
	}
	var total int64
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), dbName) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, fmt.Errorf("stat %s: %w", entry.Name(), err)
		}
		total += info.Size()
	}
	return total, nil
}

// Forget archives the rows whose content it is given. The platform never
// hard-deletes, so what a bound removes from the index stays in the store —
// which is why the footprint is reported beside the bound.
func (f *shippedFrame) Forget(ctx context.Context, held []string) error {
	if len(held) == 0 {
		return nil
	}
	active, err := f.store.Active(ctx)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	victims := map[string]bool{}
	for _, text := range held {
		victims[text] = true
	}
	for _, m := range active {
		if !victims[m.Content] {
			continue
		}
		if err := f.store.Archive(ctx, m.ID, 0, f.now); err != nil {
			return fmt.Errorf("archive %d: %w", m.ID, err)
		}
	}
	return nil
}

// Held is the content of every active row. The archive is left out: a
// search never reaches it, so a row there is not held in any sense a read
// could act on — it is only paid for, which the byte figures carry.
func (f *shippedFrame) Held(ctx context.Context) ([]string, error) {
	active, err := f.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	out := make([]string, 0, len(active))
	for _, m := range active {
		out = append(out, m.Content)
	}
	return out, nil
}

func (f *shippedFrame) Close() error { return f.svc.Close() }

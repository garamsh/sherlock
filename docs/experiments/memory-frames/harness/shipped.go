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

// shippedFrame is internal/memory behind the seam: the SQLite store this
// project ships and the reads it answers with, called through
// memory.Service rather than reimplemented.
//
// What it holds is an address and a value (ADR 0018, #582). It is
// therefore an addressed frame now rather than the sentence store the
// committed figures measured — the same seam over different contents —
// and the record's §Limits says which of its rows were taken against
// which.
//
// It writes only what the arm hands it already extracted. A turn carrying
// no fact is not written at all: an addressed store has nowhere to put a
// sentence it cannot address, and what that costs is the text arm's whole
// result. The operation comes from the arm's declaration, which is the
// same field internal/agent/memory/writeback.go carries from the model.
type shippedFrame struct {
	dir   string
	store *sqlite.Store
	svc   memory.Service
	// now is the simulated clock the retention window reads. Every entry
	// point sets it from the turn before doing anything else, so a decade
	// passes inside the run.
	now time.Time
}

// NewShippedFrame opens a store under dir and wires the shipped service
// over it.
//
// It takes no embedder and no read budget. The addressed store embeds
// nothing, and its answer is the address asked rather than a budget fill
// — ADR 0018 §Consequences: an addressed store's answer is identical from
// a fraction of the shipped token budget to four times it.
func NewShippedFrame(dir string) (Frame, error) {
	f := &shippedFrame{dir: dir}
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
	f.svc = memory.NewService(store, memory.WithClock(func() time.Time { return f.now }))
	return nil
}

// Observe writes each of the turn's assertions to its address, then runs
// the pass the gateway runs after a completed turn
// (internal/gateway/runner.go): tiered retention. Leaving it out would
// measure a lifecycle this project does not run.
func (f *shippedFrame) Observe(ctx context.Context, t Turn) error {
	f.now = t.At
	for _, written := range t.Written {
		if _, err := f.svc.Store(ctx, memory.StoreInput{
			Address:   storeAddress(written.Address),
			Value:     written.Value,
			Operation: storeOperation(written.Declared),
			Source:    "workload",
		}); err != nil {
			return fmt.Errorf("store turn %d: %w", t.Index, err)
		}
	}
	if _, err := f.svc.Retain(ctx); err != nil {
		return fmt.Errorf("retain after turn %d: %w", t.Index, err)
	}
	return nil
}

// storeAddress carries the seam's address into the store's. The seam
// calls the second field the attribute because the arms that address by
// topic put a topic noun there; the store calls it the question, because
// the addressing this project shipped puts the question there.
func storeAddress(a Address) memory.Address {
	return memory.Address{Subject: a.Subject, Question: a.Attribute, Restriction: a.Qualifier}
}

// storeOperation carries the arm's declaration into the store's. An
// undeclared write joins, which is the direction the store errs in.
func storeOperation(op Operation) memory.Operation {
	if op == OperationReplace {
		return memory.OpReplace
	}
	return memory.OpJoin
}

// Context returns what the frame holds at the address asked about, and
// nothing else. A question naming a subject and no attribute is answered
// from every address under that subject, which is the only thing an
// addressed store can do with a partial address.
//
// Scanned is the values the read touched, which is the address asked and
// the restrictions under it rather than the whole index. That is the
// column the sentence store filled with its active count, and the
// difference between the two is what addressing bought.
func (f *shippedFrame) Context(ctx context.Context, t Turn) (Injected, error) {
	f.now = t.At
	if t.Ask == nil {
		return Injected{}, nil
	}
	var (
		held []memory.Memory
		err  error
	)
	if t.Ask.Attribute == "" {
		held, err = f.svc.UnderSubject(ctx, t.Ask.Subject)
	} else {
		held, err = f.svc.At(ctx, storeAddress(*t.Ask))
	}
	if err != nil {
		return Injected{}, fmt.Errorf("read at turn %d: %w", t.Index, err)
	}
	if len(held) == 0 {
		return Injected{Scanned: 0}, nil
	}
	lines := make([]string, 0, len(held))
	for _, m := range held {
		lines = append(lines, heldLine(m))
	}
	return Injected{Text: strings.Join(lines, "\n"), Scanned: len(held)}, nil
}

// heldLine is the text the frame emits for one value, in the form every
// other addressed frame emits it, so what a read returns is compared
// like with like.
func heldLine(m memory.Memory) string {
	return line(Address{Subject: m.Address.Subject, Attribute: m.Address.Question,
		Qualifier: m.Address.Restriction}, m.Value)
}

// Size counts the active index, which is what a read reaches; the archive
// holds what the lifecycle retired and reads never see it. Stored bytes
// are both of them plus the index, which is what a decade of turns costs
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
		out.ContentBytes += int64(len(heldLine(m)))
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

// Forget archives the values whose line it is given. The platform never
// hard-deletes, so what a bound removes from the index stays in the store
// — which is why the footprint is reported beside the bound.
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
		if !victims[heldLine(m)] {
			continue
		}
		if err := f.store.Archive(ctx, m.ID, 0, f.now); err != nil {
			return fmt.Errorf("archive %d: %w", m.ID, err)
		}
	}
	return nil
}

// Held is every active value, as the line the frame would emit for it.
// The archive is left out: a read never reaches it, so a row there is not
// held in any sense a read could act on — it is only paid for, which the
// byte figures carry.
func (f *shippedFrame) Held(ctx context.Context) ([]string, error) {
	active, err := f.store.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	out := make([]string, 0, len(active))
	for _, m := range active {
		out = append(out, heldLine(m))
	}
	return out, nil
}

func (f *shippedFrame) Close() error { return f.svc.Close() }

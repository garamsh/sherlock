// Command document is arm D of the #900 validation study: an idle-time pass
// over every 8 new mids keeps a memory document of cited lines, bounded at
// 16,000 characters and shown every turn before the shipped recall
// (../../../README.md §D).
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

func main() { study.Main(New()) }

// New returns arm D.
func New() study.Arm { return arm{} }

// Setting is D's one setting.
const Setting = "b4-doc16k"

type arm struct{}

func (arm) Name() string         { return "D" }
func (arm) Settings() []string   { return []string{Setting} }
func (arm) Instructions() string { return "" }

// Build copies the shared batch-4 store and consolidates every mid, base and
// stream, in order; the document and its history go beside the store and to
// the study's evidence.
func (arm) Build(ctx context.Context, env *study.Env, setting, dir string) (study.Built, error) {
	if setting != Setting {
		return study.Built{}, fmt.Errorf("arm D has no setting %q", setting)
	}
	shared, err := env.SharedStore(4)
	if err != nil {
		return study.Built{}, err
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	path := filepath.Join(dir, "memory.db")
	mids, err := readMids(ctx, path)
	if err != nil {
		return study.Built{}, err
	}
	doc, records, err := record(ctx, env, setting, mids)
	if err != nil {
		return study.Built{}, err
	}
	data, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return study.Built{}, err
	}
	for _, p := range []string{filepath.Join(dir, "document.json"), documentPath(env, setting)} {
		if err := os.WriteFile(p, append(data, '\n'), 0o644); err != nil {
			return study.Built{}, err
		}
	}
	n, midChars, err := study.MidStats(path)
	if err != nil {
		return study.Built{}, err
	}
	var total Applied
	strict := 0
	for _, r := range records {
		total.Added += r.Applied.Added
		total.Superseded += r.Applied.Superseded
		total.Dropped += r.Applied.Dropped
		total.Rejected += r.Applied.Rejected
		total.Skipped += r.Applied.Skipped
		if r.Strict {
			strict++
		}
	}
	return study.Built{
		StoredChars: midChars + doc.Chars(),
		Vectors:     n,
		Units:       map[string]int{"mid": n, "line": len(doc.Lines)},
		Notes: map[string]any{
			"passes": len(records), "partial_passes": len(records) - strict, "applied": total,
			"lines": len(doc.Lines), "document_chars": doc.Chars(), "versions": len(doc.History),
		},
	}, nil
}

// readMids reads every mid in order with its interval and summary.
func readMids(ctx context.Context, path string) ([]Mid, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, source_first, source_last, summary FROM conversation_mid ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mid
	for rows.Next() {
		var m Mid
		if err := rows.Scan(&m.ID, &m.First, &m.Last, &m.Summary); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Open reads the store and the document Build wrote beside it.
func (arm) Open(_ context.Context, env *study.Env, _ string, dir string) (study.Memory, error) {
	data, err := os.ReadFile(filepath.Join(dir, "document.json"))
	if err != nil {
		return nil, err
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(filepath.Join(dir, "memory.db"), env.Embedder)
	if err != nil {
		return nil, err
	}
	return memory{ShippedMemory: study.ShippedMemory{Conversation: store}, doc: doc}, nil
}

// memory is the shipped memory with the document before its recall.
type memory struct {
	study.ShippedMemory
	doc Document
}

// Recall is every document line, in order, then the shipped recall.
func (m memory) Recall(ctx context.Context, task string) ([]study.Item, error) {
	shipped, err := m.ShippedMemory.Recall(ctx, task)
	if err != nil {
		return nil, err
	}
	return append(LineItems(m.doc), shipped...), nil
}

// LineItems renders the document's lines as recall items, each labelled with
// the mids it cites.
func LineItems(d Document) []study.Item {
	out := make([]study.Item, 0, len(d.Lines))
	for _, l := range d.Lines {
		out = append(out, study.Item{
			ID: int64(l.ID), Level: "line", Kind: "memory document",
			SourceFirst: l.First, SourceLast: l.Last, Text: l.Text,
			Label: fmt.Sprintf("[line:%d mids:%s; model-written]", l.ID, joinIDs(l.Mids)),
		})
	}
	return out
}

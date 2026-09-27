// Command context is arm C of issue #891, in the #890 write-time encoding
// validation: each stream batch is synthesised with the eight messages
// before it given as labelled context, and the call also writes a short
// context line, which is embedded before the summary and shown labelled on
// recall. See README.md beside it.
package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
	"github.com/garamsh/sherlock/internal/memory/sqlite"
)

func main() { study.Main(New()) }

// New returns arm C.
func New() study.Arm { return arm{} }

type arm struct{}

// setting is C's own setting: batch 4 with the 8 messages before it.
const setting = "b4-w8"

// keyOnly is C-key (#896): C's recorded context used only in the embedding
// key, before S's summary, which stays the text shown. It makes no call.
const keyOnly = "b4-w8-key"

func (arm) Name() string         { return "C" }
func (arm) Settings() []string   { return []string{setting, keyOnly} }
func (arm) Instructions() string { return "" }

// embedText is what a mid's vector is computed from: the context first, as
// Contextual Retrieval prepends it, so an embedder that cuts at its window
// cuts the summary's end rather than the context.
func embedText(contextLine, summary string) string {
	if contextLine == "" {
		return summary
	}
	return contextLine + "\n\n" + summary
}

// Build copies the shared batch-4 store and, for every stream mid, stores
// C's summary and context and re-embeds the mid from both; C-key keeps S's
// summary and re-embeds the mid from C's context and it. The base's mids
// keep their recorded summaries and have no context
// (../../preregistration.md §Arms).
func (arm) Build(ctx context.Context, env *study.Env, s, dir string) (study.Built, error) {
	if s != setting && s != keyOnly {
		return study.Built{}, fmt.Errorf("unknown setting %q", s)
	}
	recorded, err := recordContexts(ctx, env)
	if err != nil {
		return study.Built{}, err
	}
	shared, err := env.SharedStore(4)
	if err != nil {
		return study.Built{}, err
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	path := filepath.Join(dir, "memory.db")
	if err := writeContexts(ctx, env, path, recorded, s == keyOnly); err != nil {
		return study.Built{}, err
	}
	return stats(ctx, path)
}

// writeContexts adds the context column and rewrites each recorded stream
// mid: summary, context and embedding. With keepSummary (C-key) the mid's
// shared summary stays and only the context and embedding change. A batch
// recorded as failed keeps its shared mid unchanged.
func writeContexts(ctx context.Context, env *study.Env, path string, recorded map[int64]batchContext, keepSummary bool) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `ALTER TABLE conversation_mid ADD COLUMN context TEXT`); err != nil {
		return err
	}
	written := 0
	for first, b := range recorded {
		written++
		if b.Failed != "" {
			continue
		}
		summary := strings.TrimSpace(study.Redact(b.Summary))
		if keepSummary {
			err := tx.QueryRowContext(ctx, `SELECT summary FROM conversation_mid WHERE source_first=? AND source_last=?`, first, b.Last).Scan(&summary)
			if err != nil {
				return fmt.Errorf("shared summary of %d-%d: %w", first, b.Last, err)
			}
		}
		contextLine := strings.TrimSpace(study.Redact(b.Context))
		vector, err := env.Embedder.Embed(ctx, embedText(contextLine, summary))
		if err != nil {
			return fmt.Errorf("embed mid at %d: %w", first, err)
		}
		res, err := tx.ExecContext(ctx, `UPDATE conversation_mid SET summary=?, context=?, embedding=? WHERE source_first=? AND source_last=?`,
			summary, contextLine, encodeVector(vector), first, b.Last)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("no mid covers %d-%d in the shared store", first, b.Last)
		}
	}
	if written != len(env.Corpus.Stream)/4 {
		return fmt.Errorf("%d stream batches recorded; the stream has %d", written, len(env.Corpus.Stream)/4)
	}
	return tx.Commit()
}

// encodeVector is the production store's vector encoding
// (internal/memory/sqlite encodeVector, unexported): little-endian float32.
func encodeVector(vector []float32) []byte {
	out := make([]byte, 4*len(vector))
	for i, x := range vector {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

// stats counts what C stores: every summary and every context, one vector
// per mid.
func stats(ctx context.Context, path string) (study.Built, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return study.Built{}, err
	}
	defer db.Close()
	var mids, chars, contexts, contextChars int
	err = db.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(length(summary)),0), count(nullif(context,'')), coalesce(sum(length(context)),0) FROM conversation_mid`).
		Scan(&mids, &chars, &contexts, &contextChars)
	if err != nil {
		return study.Built{}, err
	}
	return study.Built{
		StoredChars: chars + contextChars,
		Vectors:     mids,
		Units:       map[string]int{"mid": mids, "context": contexts},
		Notes:       map[string]any{"context_chars": contextChars, "mids_with_context": contexts},
	}, nil
}

// readContexts returns every mid's stored context, empty ones left out.
func readContexts(ctx context.Context, path string) (map[int64]string, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id, context FROM conversation_mid WHERE context <> ''`)
	if err != nil {
		return nil, fmt.Errorf("read contexts: %w", err)
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var c string
		if err := rows.Scan(&id, &c); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// Open reads the contexts and opens the production store under them.
// C-key shows no context: its recall and search are S's over its keys.
func (arm) Open(ctx context.Context, env *study.Env, s, dir string) (study.Memory, error) {
	if s != setting && s != keyOnly {
		return nil, fmt.Errorf("unknown setting %q", s)
	}
	path := filepath.Join(dir, "memory.db")
	if s == keyOnly {
		store, err := sqlite.OpenConversation(path, env.Embedder)
		if err != nil {
			return nil, err
		}
		return study.ShippedMemory{Conversation: store}, nil
	}
	contexts, err := readContexts(ctx, path)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return nil, err
	}
	return memory{shipped: study.ShippedMemory{Conversation: store}, contexts: contexts}, nil
}

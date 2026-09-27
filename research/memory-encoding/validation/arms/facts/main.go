// Command facts is arm F of the #890 validation study: the synthesis call
// also writes up to five standing facts per batch, and recall adds a
// profile block of the newest facts about the user and the facts most
// similar to the task. See README.md beside it.
package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/memory/sqlite"
	"github.com/garamsh/sherlock/research/chained-recall/study"
)

func main() { study.Main(New()) }

// New returns arm F.
func New() study.Arm { return arm{} }

type arm struct{}

func (arm) Name() string         { return "F" }
func (arm) Settings() []string   { return []string{"b4"} }
func (arm) Instructions() string { return "" }

// factSchema is the table F adds to the production store: each fact a
// mid's synthesis wrote, its kind and its embedding.
const factSchema = `CREATE TABLE mid_fact (
 id INTEGER PRIMARY KEY,
 mid INTEGER NOT NULL REFERENCES conversation_mid(id),
 kind TEXT NOT NULL CHECK (kind IN ('user','other')),
 text TEXT NOT NULL,
 embedding BLOB NOT NULL
)`

// Build copies the shared batch-4 store and writes the facts F's synthesis
// wrote for each stream mid beside it, each embedded by the local embedder.
func (arm) Build(ctx context.Context, env *study.Env, _ string, dir string) (study.Built, error) {
	shared, err := env.SharedStore(4)
	if err != nil {
		return study.Built{}, err
	}
	facts, err := recordFacts(ctx, env)
	if err != nil {
		return study.Built{}, err
	}
	if err := study.CopyDir(shared, dir); err != nil {
		return study.Built{}, err
	}
	path := filepath.Join(dir, "memory.db")
	written, users, chars, err := writeFacts(ctx, env, path, facts)
	if err != nil {
		return study.Built{}, err
	}
	mids, midChars, err := study.MidStats(path)
	if err != nil {
		return study.Built{}, err
	}
	batches := len(env.Corpus.Stream) / 4
	return study.Built{
		StoredChars: midChars + chars,
		Vectors:     mids + written,
		Units:       map[string]int{"mid": mids, "fact": written},
		Notes: map[string]any{
			"stream_batches": batches, "batches_recorded": len(facts),
			"facts": written, "user_facts": users,
		},
	}, nil
}

// writeFacts adds the fact table to the store at path and writes every
// recorded fact to the mid over its batch, in source order.
func writeFacts(ctx context.Context, env *study.Env, path string, facts map[int64][]Fact) (written, users, chars int, err error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, factSchema); err != nil {
		return 0, 0, 0, err
	}
	stream := env.Corpus.Stream
	for i := 0; i < len(stream); i += 4 {
		first := stream[i].ID
		var mid int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM conversation_mid WHERE source_first=? AND source_last=?`, first, first+3).Scan(&mid); err != nil {
			return 0, 0, 0, fmt.Errorf("mid over %d-%d: %w", first, first+3, err)
		}
		for _, f := range facts[first] {
			text := study.Redact(f.Text)
			vector, err := env.Embedder.Embed(ctx, text)
			if err != nil {
				return 0, 0, 0, err
			}
			if _, err := db.ExecContext(ctx, `INSERT INTO mid_fact(mid,kind,text,embedding) VALUES (?,?,?,?)`, mid, f.Kind, text, encode(vector)); err != nil {
				return 0, 0, 0, err
			}
			written++
			chars += len([]rune(text))
			if f.Kind == "user" {
				users++
			}
		}
	}
	return written, users, chars, nil
}

func encode(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decode(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// Open reads the production store and F's facts beside it.
func (arm) Open(ctx context.Context, env *study.Env, _ string, dir string) (study.Memory, error) {
	path := filepath.Join(dir, "memory.db")
	facts, err := readFacts(ctx, path)
	if err != nil {
		return nil, err
	}
	store, err := sqlite.OpenConversation(path, env.Embedder)
	if err != nil {
		return nil, err
	}
	return &memory{ShippedMemory: study.ShippedMemory{Conversation: store}, embedder: env.Embedder, facts: facts}, nil
}

// readFacts reads every fact, newest mid first and, within a mid, in the
// order written.
func readFacts(ctx context.Context, path string) ([]storedFact, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT f.id, f.kind, f.text, f.embedding, m.source_first, m.source_last
 FROM mid_fact f JOIN conversation_mid m ON m.id = f.mid ORDER BY f.mid DESC, f.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedFact
	for rows.Next() {
		var f storedFact
		var blob []byte
		if err := rows.Scan(&f.ID, &f.Kind, &f.Text, &blob, &f.First, &f.Last); err != nil {
			return nil, err
		}
		f.Vector = decode(blob)
		out = append(out, f)
	}
	return out, rows.Err()
}

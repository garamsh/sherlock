package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/garamsh/gagent/internal/memory"
)

// schema holds the DDL. The active table is searchable; the archive table
// preserves superseded, evicted, compressed, and forgotten rows. The FTS
// index is external-content over the active table, kept in sync by
// triggers so archived rows drop out of keyword search automatically.
const schema = `
CREATE TABLE IF NOT EXISTS memories_active (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	content TEXT NOT NULL,
	embedding BLOB NOT NULL,
	type TEXT NOT NULL CHECK (type IN ('episodic', 'semantic')),
	importance_score REAL NOT NULL,
	access_count INTEGER NOT NULL DEFAULT 0,
	last_accessed INTEGER,
	timestamp INTEGER NOT NULL,
	source TEXT NOT NULL,
	superseded_by INTEGER,
	valid_to INTEGER
);
CREATE TABLE IF NOT EXISTS memories_archive (
	id INTEGER PRIMARY KEY,
	content TEXT NOT NULL,
	embedding BLOB NOT NULL,
	type TEXT NOT NULL,
	importance_score REAL NOT NULL,
	access_count INTEGER NOT NULL,
	last_accessed INTEGER,
	timestamp INTEGER NOT NULL,
	source TEXT NOT NULL,
	superseded_by INTEGER,
	valid_to INTEGER
);
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
	content,
	content = 'memories_active',
	content_rowid = 'id'
);
CREATE TRIGGER IF NOT EXISTS memories_active_ai AFTER INSERT ON memories_active BEGIN
	INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS memories_active_ad AFTER DELETE ON memories_active BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;
`

const columns = `id, content, embedding, type, importance_score, access_count,
	last_accessed, timestamp, source, superseded_by, valid_to`

// Store is the SQLite-backed memory.Store.
type Store struct {
	db *sql.DB
}

var _ memory.Store = (*Store)(nil)

// Open opens (creating on first use) the memory.db at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close implements memory.Store.
func (s *Store) Close() error { return s.db.Close() }

// Insert implements memory.Store.
func (s *Store) Insert(ctx context.Context, m memory.Memory) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO memories_active (content, embedding, type, importance_score, timestamp, source)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.Content, encodeEmbedding(m.Embedding), string(m.Type),
		m.ImportanceScore, m.Timestamp.Unix(), m.Source)
	if err != nil {
		return 0, fmt.Errorf("insert memory: %w", err)
	}
	return res.LastInsertId()
}

// Get implements memory.Store. Only active memories are visible.
func (s *Store) Get(ctx context.Context, id int64) (memory.Memory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+columns+` FROM memories_active WHERE id = ?`, id)
	m, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Memory{}, fmt.Errorf("id %d: %w", id, memory.ErrNotFound)
	}
	if err != nil {
		return memory.Memory{}, fmt.Errorf("get %d: %w", id, err)
	}
	return m, nil
}

// Active implements memory.Store.
func (s *Store) Active(ctx context.Context) ([]memory.Memory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM memories_active`)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	defer rows.Close()
	var out []memory.Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan active: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FTSRanks implements memory.Store. The query is tokenized and each token
// is quoted and OR-ed, so agent-supplied text can never inject FTS5
// syntax.
func (s *Store) FTSRanks(ctx context.Context, query string) (map[int64]float64, error) {
	terms := tokenize(query)
	if len(terms) == 0 {
		return map[int64]float64{}, nil
	}
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT rowid, rank FROM memories_fts WHERE memories_fts MATCH ?`,
		strings.Join(quoted, " OR "))
	if err != nil {
		return nil, fmt.Errorf("fts match: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]float64)
	for rows.Next() {
		var id int64
		var rank float64
		if err := rows.Scan(&id, &rank); err != nil {
			return nil, fmt.Errorf("scan fts: %w", err)
		}
		out[id] = rank
	}
	return out, rows.Err()
}

// Archive implements memory.Store: the row moves to the archive table
// with superseded_by and valid_to recorded; nothing is hard-deleted.
func (s *Store) Archive(ctx context.Context, id int64, supersededBy int64, validTo time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive %d: %w", id, err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO memories_archive (`+columns+`)
		 SELECT `+columns+` FROM memories_active WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("copy to archive %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("id %d: %w", id, memory.ErrNotFound)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE memories_archive SET superseded_by = ?, valid_to = ? WHERE id = ?`,
		nullID(supersededBy), validTo.Unix(), id); err != nil {
		return fmt.Errorf("mark archived %d: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM memories_active WHERE id = ?`, id); err != nil {
		return fmt.Errorf("remove active %d: %w", id, err)
	}
	return tx.Commit()
}

// RecordAccess implements memory.Store.
func (s *Store) RecordAccess(ctx context.Context, ids []int64, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record access: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE memories_active SET access_count = access_count + 1, last_accessed = ? WHERE id = ?`,
			at.Unix(), id); err != nil {
			return fmt.Errorf("record access %d: %w", id, err)
		}
	}
	return tx.Commit()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMemory(row scanner) (memory.Memory, error) {
	var (
		m          memory.Memory
		typ        string
		emb        []byte
		lastAccess sql.NullInt64
		timestamp  int64
		superseded sql.NullInt64
		validTo    sql.NullInt64
	)
	err := row.Scan(&m.ID, &m.Content, &emb, &typ, &m.ImportanceScore,
		&m.AccessCount, &lastAccess, &timestamp, &m.Source, &superseded, &validTo)
	if err != nil {
		return memory.Memory{}, err
	}
	m.Type = memory.Type(typ)
	m.Embedding = decodeEmbedding(emb)
	m.Timestamp = time.Unix(timestamp, 0).UTC()
	if lastAccess.Valid {
		m.LastAccessed = time.Unix(lastAccess.Int64, 0).UTC()
	}
	if superseded.Valid {
		m.SupersededBy = superseded.Int64
	}
	if validTo.Valid {
		m.ValidTo = time.Unix(validTo.Int64, 0).UTC()
	}
	return m, nil
}

func encodeEmbedding(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
	}
	return buf
}

func decodeEmbedding(buf []byte) []float32 {
	out := make([]float32, len(buf)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i:]))
	}
	return out
}

// nullID maps the service's "0 means none" convention to SQL NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// tokenize mirrors memory's tokenization so FTS queries match what the
// embedder saw.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

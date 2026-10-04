package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/memory"
)

// schema holds the DDL. The active table is readable; the archive table
// preserves superseded, evicted and forgotten rows.
//
// One index carries every read. Its leading columns are the address —
// subject, then question, then restriction — so the three reads are the
// three prefixes of one key, and its trailing value column is what makes
// a value held once at an address held once (ADR 0018). There is no
// embedding column and no keyword index: an addressed store reaches its
// address and needs neither.
const schema = `
CREATE TABLE IF NOT EXISTS memories_active (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	subject TEXT NOT NULL,
	question TEXT NOT NULL,
	restriction TEXT NOT NULL,
	value TEXT NOT NULL,
	importance_score REAL NOT NULL,
	access_count INTEGER NOT NULL DEFAULT 0,
	last_accessed INTEGER,
	timestamp INTEGER NOT NULL,
	source TEXT NOT NULL,
	superseded_by INTEGER,
	valid_to INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS memories_active_address
	ON memories_active (subject, question, restriction, value);
CREATE TABLE IF NOT EXISTS memories_archive (
	id INTEGER PRIMARY KEY,
	subject TEXT NOT NULL,
	question TEXT NOT NULL,
	restriction TEXT NOT NULL,
	value TEXT NOT NULL,
	importance_score REAL NOT NULL,
	access_count INTEGER NOT NULL,
	last_accessed INTEGER,
	timestamp INTEGER NOT NULL,
	source TEXT NOT NULL,
	superseded_by INTEGER,
	valid_to INTEGER
);
`

const columns = `id, subject, question, restriction, value, importance_score,
	access_count, last_accessed, timestamp, source, superseded_by, valid_to`

// ErrPreAddressedStore reports a memory.db written before #582, which
// holds sentences rather than addresses. It is a distinct error because
// there is no migration to offer: computing an address from a stored
// sentence is a model call, and the store does not make one.
var ErrPreAddressedStore = errors.New("memory.db holds the pre-addressed schema")

// ErrAddressedStore requires an explicit migration before source-linked recall.
var ErrAddressedStore = errors.New("memory.db holds addressed memories without source links")

// ErrStoreLocked reports that another process holds the store's writer
// lock, so this one must not open the file (ADR 0002).
var ErrStoreLocked = errors.New("another process owns the memory store")

// Store is the SQLite-backed memory.Storer.
type Store struct {
	db       *sql.DB
	embedder agent.Embedder
	// lock is the writer lock taken before db opened; closing it
	// releases the lock.
	lock *os.File
}

var _ memory.Storer = (*Store)(nil)

// Open takes the store's writer lock, then opens (creating on first use)
// the memory.db at path. It returns ErrStoreLocked when another process
// holds the lock.
func Open(path string) (*Store, error) {
	lock, err := lockWriter(path)
	if err != nil {
		return nil, err
	}
	store, err := open(path, lock)
	if err != nil {
		lock.Close()
		return nil, err
	}
	return store, nil
}

// open opens the memory.db at path for a caller that already holds its
// writer lock. The caller releases the lock when open fails.
func open(path string, lock *os.File) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	legacy, err := holdsSentences(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("inspect %q: %w", path, err)
	}
	if legacy {
		db.Close()
		return nil, fmt.Errorf("%q: %w", path, ErrPreAddressedStore)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := prepareConversation(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create conversation schema: %v", err)
	}
	return &Store{db: db, lock: lock}, nil
}

// holdsSentences reports whether the file already carries the sentence
// store's active table. An empty or absent file carries no table at all
// and is not one.
func holdsSentences(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT count(*) FROM pragma_table_info('memories_active') WHERE name = 'content'`,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("read table info: %w", err)
	}
	return count > 0, nil
}

// Close implements memory.Storer. The writer lock is released after the
// database closes, so the next opener never meets an unclosed handle. A
// second Close returns nil, as sql.DB's does.
func (s *Store) Close() error {
	dbErr := s.db.Close()
	lockErr := s.lock.Close()
	if errors.Is(lockErr, os.ErrClosed) {
		lockErr = nil
	}
	return errors.Join(dbErr, lockErr)
}

// Write implements memory.Storer. A value already active at the address
// is not stored twice: the write moves its timestamp and returns the row
// that was already there, so restating a fact holds it once.
func (s *Store) Write(ctx context.Context, m memory.Memory, supersede []int64, at time.Time) (int64, []int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("begin write: %w", err)
	}
	defer tx.Rollback()

	// RETURNING rather than LastInsertId, which reports the connection's
	// last inserted rowid and so reports a stale row when the upsert
	// takes its update branch.
	//
	// Salience is raised and never lowered: a value restated by a join
	// after a replacement wrote it keeps the correction's tier, which is
	// what ADR 0006 asks of a correction's salience.
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO memories_active
			(subject, question, restriction, value, importance_score, timestamp, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (subject, question, restriction, value) DO UPDATE SET
			timestamp = excluded.timestamp,
			importance_score = max(importance_score, excluded.importance_score),
			source = excluded.source
		 RETURNING id`,
		m.Address.Subject, m.Address.Question, m.Address.Restriction, m.Value,
		m.ImportanceScore, m.Timestamp.Unix(), m.Source).Scan(&id)
	if err != nil {
		return 0, nil, fmt.Errorf("write memory: %w", err)
	}

	archived := make([]int64, 0, len(supersede))
	for _, old := range supersede {
		// The row just written is never archived against itself, which
		// is what a replacement restating the value it replaces asks
		// for: the value stays, at the new timestamp.
		if old == id {
			continue
		}
		if err := archiveTx(ctx, tx, old, id, at); err != nil {
			return 0, nil, err
		}
		archived = append(archived, old)
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, fmt.Errorf("commit write: %w", err)
	}
	if len(archived) == 0 {
		return id, nil, nil
	}
	return id, archived, nil
}

// At implements memory.Storer.
func (s *Store) At(ctx context.Context, a memory.Address) ([]memory.Memory, error) {
	return s.query(ctx,
		`WHERE subject = ? AND question = ? AND restriction = ?`,
		a.Subject, a.Question, a.Restriction)
}

// UnderQuestion implements memory.Storer.
func (s *Store) UnderQuestion(ctx context.Context, subject, question string) ([]memory.Memory, error) {
	return s.query(ctx, `WHERE subject = ? AND question = ?`, subject, question)
}

// UnderSubject implements memory.Storer.
func (s *Store) UnderSubject(ctx context.Context, subject string) ([]memory.Memory, error) {
	return s.query(ctx, `WHERE subject = ?`, subject)
}

// Active implements memory.Storer.
func (s *Store) Active(ctx context.Context) ([]memory.Memory, error) {
	return s.query(ctx, ``)
}

// query runs one read over the active table. The order is the address
// index's own — restriction, then insertion — so a read is repeatable and
// a caller reading two addresses gets the narrower one after the wider.
// It is a traversal order and not a ranking: nothing here scores a row,
// and an addressed store has no ordering to offer over the addresses a
// question spans (ADR 0018 §What is not measured).
func (s *Store) query(ctx context.Context, where string, args ...any) ([]memory.Memory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+columns+` FROM memories_active `+where+` ORDER BY restriction, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read active: %w", err)
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

// Archive implements memory.Storer: the row moves to the archive table
// with superseded_by and valid_to recorded; nothing is hard-deleted.
func (s *Store) Archive(ctx context.Context, id int64, supersededBy int64, validTo time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive %d: %w", id, err)
	}
	defer tx.Rollback()
	if err := archiveTx(ctx, tx, id, supersededBy, validTo); err != nil {
		return err
	}
	return tx.Commit()
}

// archiveTx moves one active row to the archive inside tx. It is shared
// by Archive and by the supersessions Write performs, so a superseded
// row and a retired one reach the archive by the same statements.
func archiveTx(ctx context.Context, tx *sql.Tx, id int64, supersededBy int64, validTo time.Time) error {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO memories_archive (`+columns+`)
		 SELECT `+columns+` FROM memories_active WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("copy to archive %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("copy to archive %d: %w", id, err)
	}
	if n == 0 {
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
	return nil
}

// RecordAccess implements memory.Storer.
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
		lastAccess sql.NullInt64
		timestamp  int64
		superseded sql.NullInt64
		validTo    sql.NullInt64
	)
	err := row.Scan(&m.ID, &m.Address.Subject, &m.Address.Question, &m.Address.Restriction,
		&m.Value, &m.ImportanceScore, &m.AccessCount, &lastAccess, &timestamp,
		&m.Source, &superseded, &validTo)
	if err != nil {
		return memory.Memory{}, err
	}
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

// nullID maps the service's "0 means none" convention to SQL NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

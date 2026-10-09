package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "modernc.org/sqlite"

	"github.com/garamsh/sherlock/internal/agent"
)

// ErrAddressedStore requires an explicit migration before source-linked recall.
var ErrAddressedStore = errors.New("memory.db holds addressed memories without source links")

// ErrStoreLocked reports that another process holds the store's writer
// lock, so this one must not open the file (ADR 0002).
var ErrStoreLocked = errors.New("another process owns the memory store")

// Store is the SQLite-backed agent.ConversationMemory.
type Store struct {
	db       *sql.DB
	embedder agent.Embedder
	// lock is the writer lock taken before db opened; closing it
	// releases the lock.
	lock *os.File
}

// open opens the memory.db at path for a caller that already holds its
// writer lock. The caller releases the lock when open fails.
func open(path string, lock *os.File) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	if err := prepareConversation(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create conversation schema: %v", err)
	}
	return &Store{db: db, lock: lock}, nil
}

// Close releases the store. The writer lock is released after the
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

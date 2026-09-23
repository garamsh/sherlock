package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/garamsh/gagent/internal/memory"
)

// createWorkingContext adds the singleton without resetting an existing account.
func createWorkingContext(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin working context migration: %v", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'working_context'`).Scan(&count); err != nil {
		return fmt.Errorf("inspect working context: %v", err)
	}
	if count == 0 {
		if _, err := tx.Exec(`CREATE TABLE working_context (id INTEGER PRIMARY KEY CHECK (id = 1), version INTEGER NOT NULL, revision INTEGER NOT NULL, document BLOB NOT NULL);
   INSERT INTO working_context VALUES (1, 4, 0, '{"account":{"status":"idle","progress":""},"source":[],"arrival":null,"attempts":[]}');`); err != nil {
			return fmt.Errorf("create working context: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit working context migration: %v", err)
	}
	return nil
}

// LoadWorkingContext implements memory.WorkingContextStorer.
func (s *Store) LoadWorkingContext(ctx context.Context) (memory.WorkingContext, error) {
	var version int
	var revision int64
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT version, revision, document FROM working_context WHERE id = 1`).Scan(&version, &revision, &data); err != nil {
		return memory.WorkingContext{}, fmt.Errorf("load working context: %v", err)
	}
	if version != 4 || revision < 0 {
		return memory.WorkingContext{}, fmt.Errorf("invalid working context version or revision: %d/%d", version, revision)
	}
	work, err := memory.ParseWorkingContext(data)
	if err != nil {
		return memory.WorkingContext{}, err
	}
	work.Revision = revision
	return work, nil
}

// SaveWorkingContext implements memory.WorkingContextStorer.
func (s *Store) SaveWorkingContext(ctx context.Context, work memory.WorkingContext) (int64, error) {
	data, err := json.Marshal(work)
	if err != nil {
		return 0, fmt.Errorf("encode working context: %v", err)
	}
	if _, err := memory.ParseWorkingContext(data); err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE working_context SET document = ?, revision = revision + 1 WHERE id = 1 AND revision = ? AND version = 4`, data, work.Revision)
	if err != nil {
		return 0, fmt.Errorf("save working context: %v", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("save working context count: %v", err)
	}
	if changed != 1 {
		return 0, fmt.Errorf("stale working context")
	}
	return work.Revision + 1, nil
}

package memory

import (
	"context"
	"fmt"
)

// Delete implements the delete operation: the active memory moves to the
// archive with valid_to recorded and superseded_by left at 0 — retirement,
// not supersession. Delete is not idempotent: a missing or
// already-archived ID returns ErrNotFound.
func (s *service) Delete(ctx context.Context, id int64) error {
	if err := s.store.Archive(ctx, id, 0, s.clock()); err != nil {
		return fmt.Errorf("archive %d: %w", id, err)
	}
	return nil
}

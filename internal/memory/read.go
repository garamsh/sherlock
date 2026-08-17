package memory

import (
	"context"
	"fmt"
)

// At implements the addressed read: the values held at the address asked
// and, where that address restricts nothing, the values at every address
// restricting the same question (ADR 0018).
//
// The widening is one relation between addresses rather than a rule per
// case — a narrower question is a question about the same thing — and
// without it a value written at a restricted address would sit where
// nothing ever asks for it. An address that already carries a restriction
// is answered by itself: it is the narrow question, and the addresses
// under it are not.
//
// What comes back is not ranked and not trimmed. There is nothing to rank
// it by: the store holds an address and a value, and the ordering a
// question spanning several addresses would need is not in the record
// (ADR 0018 §What is not measured).
func (s *service) At(ctx context.Context, a Address) ([]Memory, error) {
	var (
		held []Memory
		err  error
	)
	if a.Restriction == "" {
		held, err = s.store.UnderQuestion(ctx, a.Subject, a.Question)
	} else {
		held, err = s.store.At(ctx, a)
	}
	if err != nil {
		return nil, fmt.Errorf("read address: %w", err)
	}
	if err := s.recordAccess(ctx, held); err != nil {
		return nil, err
	}
	return held, nil
}

// UnderSubject implements the partial-address read: a question naming a
// subject and no question is answered from every address under that
// subject, which is the only thing an addressed store can do with one.
//
// Nothing bounds what comes back. A store holding an address and a value
// has no ranking to order the addresses by and no budget to stop at, so
// where the subject spans more than fits it returns more than fits
// (ADR 0018 §What is not measured). Deciding what to leave out is the
// read step's, and specifying it is not in the record.
func (s *service) UnderSubject(ctx context.Context, subject string) ([]Memory, error) {
	held, err := s.store.UnderSubject(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("read subject: %w", err)
	}
	if err := s.recordAccess(ctx, held); err != nil {
		return nil, err
	}
	return held, nil
}

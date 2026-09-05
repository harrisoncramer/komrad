package agents

import (
	"context"
	"fmt"

	"weave/internal/store"
	markstore "weave/internal/store/mark"
)

type MarkServiceParams struct {
	Marks *markstore.Store
	Tx    txRunner
}

// MarkService owns the letters a person addresses rows by, which span agents and workflows and
// so are written through a transaction rather than a single store.
type MarkService struct {
	marks *markstore.Store
	tx    txRunner
}

// NewMarkService builds the service over the mark store and the transaction it writes through.
func NewMarkService(p MarkServiceParams) *MarkService {
	return &MarkService{marks: p.Marks, tx: p.Tx}
}

// ListMarks reads every mark the panel draws.
func (s *MarkService) ListMarks(ctx context.Context) ([]markstore.Mark, error) {
	return s.marks.List(ctx)
}

// SetMark puts a letter on a live row, freeing the letter and whatever that row already
// answered to first, and reports whether the row was there to take it.
func (s *MarkService) SetMark(ctx context.Context, mark string, target markstore.Target) (bool, error) {
	if !target.IsID() {
		return false, nil
	}

	var marked bool
	err := s.tx.InTx(ctx, func(db store.DBTX) error {
		marks := markstore.New(db)
		// Freeing both before claiming is what keeps one letter on one row and one row on one
		// letter, and the transaction is what stops a second caller landing between the two.
		if err := marks.ClearConflicts(ctx, mark, target); err != nil {
			return err
		}

		var err error
		marked, err = marks.Set(ctx, mark, target)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("failed to set mark %q: %w", mark, err)
	}

	return marked, nil
}

// ClearMark takes whatever letter a row answers to off it.
func (s *MarkService) ClearMark(ctx context.Context, target markstore.Target) (bool, error) {
	if !target.IsID() {
		return false, nil
	}

	return s.marks.Clear(ctx, target)
}

package marks

import (
	"cmp"
	"context"
	"fmt"

	"weave/db"
	"weave/internal/store"
	markstore "weave/internal/store/mark"
	"weave/internal/watch"
)

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

type NewMarkServiceParams struct {
	Marks *markstore.Store
	Tx    txRunner
	// Watch is told which rows a mark moved between, watch.Discard when unset.
	Watch watch.Publisher
}

// MarkService owns the letters a person types to jump to a row, which span agents and workflows
// and so are written through a transaction rather than a single store.
type MarkService struct {
	marks *markstore.Store
	tx    txRunner
	watch watch.Publisher
}

// NewMarkService builds the service over the mark store and the transaction it writes through.
func NewMarkService(params NewMarkServiceParams) *MarkService {
	return &MarkService{marks: params.Marks, tx: params.Tx, watch: cmp.Or(params.Watch, watch.Discard)}
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

	// Two callers claiming a letter nobody holds each find nothing to free and so neither blocks
	// the other, so the loser meets the primary key and retries, its clear now seeing the
	// winner's row.
	marked, err := s.setMarkOnce(ctx, mark, target)
	if db.IsUniqueViolation(err) {
		return s.setMarkOnce(ctx, mark, target)
	}

	return marked, err
}

func (s *MarkService) setMarkOnce(ctx context.Context, mark string, target markstore.Target) (bool, error) {
	var marked bool
	var freed []markstore.Target
	err := s.tx.InTx(ctx, func(tx store.DBTX) error {
		marks := markstore.New(tx)
		// Freeing both before claiming is what keeps one letter on one row and one row on one
		// letter, with the transaction keeping a half-done pair off the screen rather than
		// keeping a second caller out, which the retry above is for.
		var err error
		freed, err = marks.ClearConflicts(ctx, mark, target)
		if err != nil {
			return err
		}

		marked, err = marks.Set(ctx, mark, target)

		return err
	})
	if err != nil {
		return false, fmt.Errorf("failed to set mark %q: %w", mark, err)
	}

	s.publish(append(freed, target))

	return marked, nil
}

// publish says every row the letters moved between has changed.
func (s *MarkService) publish(targets []markstore.Target) {
	for _, target := range targets {
		switch {
		case target.AgentID != nil:
			s.watch.TouchAgents(*target.AgentID)
		case target.WorkflowID != nil:
			s.watch.TouchWorkflows(*target.WorkflowID)
		}
	}
}

// ClearMark takes whatever letter a row answers to off it.
func (s *MarkService) ClearMark(ctx context.Context, target markstore.Target) (bool, error) {
	if !target.IsID() {
		return false, nil
	}

	cleared, err := s.marks.Clear(ctx, target)
	if err != nil {
		return false, err
	}

	s.publish([]markstore.Target{target})

	return cleared, nil
}

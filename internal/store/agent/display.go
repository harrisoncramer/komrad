package agent

import (
	"context"
	"fmt"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// SetMarkResult is what setting a mark changed: whether the agent took it, and the agents
// that held the letter before and lost it.
type SetMarkResult struct {
	Marked      bool
	ClearedFrom []string
}

// SetMark puts a mark on a live agent and takes the letter off whichever agent held it, or
// clears the agent's own mark when the mark is unset.
func (s *Store) SetMark(ctx context.Context, id string, mark *string) (SetMarkResult, error) {
	if !store.IsID(id) {
		return SetMarkResult{}, nil
	}

	// Two callers claiming one letter can each free it before either claims it, which loses the
	// second on the unique index and is why it retries, its clear now seeing the winner's row.
	result, err := s.setMarkOnce(ctx, id, mark)
	if db.IsUniqueViolation(err) {
		return s.setMarkOnce(ctx, id, mark)
	}

	return result, err
}

func (s *Store) setMarkOnce(ctx context.Context, id string, mark *string) (SetMarkResult, error) {
	// Freeing the letter before claiming it keeps the index satisfied within one call and leaves
	// it unheld rather than twice held on a half failure, and the clear only lands when the
	// agent named is live, so a mark aimed at an archived row takes the letter off nobody.
	var cleared []string
	if mark != nil {
		rows, err := s.queries.ClearAgentMark(ctx, models.ClearAgentMarkParams{Mark: mark, KeepID: id})
		if err != nil {
			return SetMarkResult{}, fmt.Errorf("failed to clear mark %q: %w", *mark, err)
		}
		for _, row := range rows {
			cleared = append(cleared, row.Name)
		}
	}

	updated, err := s.queries.SetAgentMark(ctx, models.SetAgentMarkParams{ID: id, Mark: mark})
	if err != nil {
		return SetMarkResult{}, fmt.Errorf("failed to mark agent %q: %w", id, err)
	}

	return SetMarkResult{Marked: len(updated) > 0, ClearedFrom: cleared}, nil
}

// SetHidden updates a live agent's hidden state.
func (s *Store) SetHidden(ctx context.Context, id string, hidden bool) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	updated, err := s.queries.SetAgentHidden(ctx, models.SetAgentHiddenParams{ID: id, Hidden: hidden})
	if err != nil {
		return false, fmt.Errorf("failed to hide agent %q: %w", id, err)
	}

	return len(updated) > 0, nil
}

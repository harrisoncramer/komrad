package agent

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
)

// SetTags updates the tag on the given agents and returns the number changed.
func (s *Store) SetTags(ctx context.Context, ids []string, tag *string) (int, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return 0, nil
	}

	updated, err := s.queries.SetAgentTags(ctx, models.SetAgentTagsParams{Ids: storable, Tag: tag})
	if err != nil {
		return 0, fmt.Errorf("failed to tag %d agents: %w", len(storable), err)
	}

	return len(updated), nil
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

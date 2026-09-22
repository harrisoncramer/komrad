package agent

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
)

// SetHidden updates a live agent's hidden state.
func (s *AgentStore) SetHidden(ctx context.Context, id string, hidden bool) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	updated, err := s.queries.SetAgentHidden(ctx, models.SetAgentHiddenParams{ID: id, Hidden: hidden})
	if err != nil {
		return false, fmt.Errorf("failed to hide agent %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return len(updated) > 0, nil
}

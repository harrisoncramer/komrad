package agent

import (
	"context"
	"fmt"

	"komrad/db/models"

	"github.com/google/uuid"
)

// SetFailureReason records why an agent's create or cleanup failed and reports whether the agent
// was there to take it.
func (s *AgentStore) SetFailureReason(ctx context.Context, id uuid.UUID, reason string) (bool, error) {
	written, err := s.queries.SetAgentFailureReason(ctx, models.SetAgentFailureReasonParams{
		ID:            id,
		FailureReason: reason,
	})
	if err != nil {
		return false, fmt.Errorf("failed to record the failure reason on agent %q: %w", id, err)
	}

	return len(written) > 0, nil
}

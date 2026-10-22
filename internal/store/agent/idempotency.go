package agent

import (
	"context"
	"fmt"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"

	"github.com/google/uuid"
)

// IdempotencyKey is a caller's key together with the requester it is scoped to.
type IdempotencyKey struct {
	Key                string
	RequestedByAgentID *uuid.UUID
}

// GetByIdempotencyKey returns the live or archived agent recorded under a key.
func (s *AgentStore) GetByIdempotencyKey(ctx context.Context, key IdempotencyKey) (*Agent, error) {
	if key.Key == "" {
		return nil, fmt.Errorf("%w: agent for idempotency key %q", store.ErrNotFound, key.Key)
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, models.GetAgentByIdempotencyKeyParams{
		IdempotencyKey:     &key.Key,
		RequestedByAgentID: key.RequestedByAgentID,
	})
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: agent for idempotency key %q", store.ErrNotFound, key.Key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key.Key, err)
	}

	return xptr.Ptr(FromRow(*row)), nil
}

// ReleaseIdempotencyKey takes a key back off the failed create holding it and reports whether
// it did, checking the agent's state in the same statement.
func (s *AgentStore) ReleaseIdempotencyKey(ctx context.Context, key IdempotencyKey) (bool, error) {
	if key.Key == "" {
		return false, nil
	}

	released, err := s.queries.ReleaseAgentIdempotencyKey(ctx, models.ReleaseAgentIdempotencyKeyParams{
		IdempotencyKey:     &key.Key,
		RequestedByAgentID: key.RequestedByAgentID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to release idempotency key %q: %w", key.Key, err)
	}

	return len(released) > 0, nil
}

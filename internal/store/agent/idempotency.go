package agent

import (
	"context"
	"fmt"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
)

// GetByIdempotencyKey returns the live or archived agent a spawner recorded under a key, a nil
// spawner naming the agents a person made.
func (s *AgentStore) GetByIdempotencyKey(ctx context.Context, key string, spawnedBy *string) (*Agent, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: agent for idempotency key %q", store.ErrNotFound, key)
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, models.GetAgentByIdempotencyKeyParams{
		IdempotencyKey:   &key,
		SpawnedByAgentID: spawnedBy,
	})
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: agent for idempotency key %q", store.ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key, err)
	}

	return xptr.Ptr(FromRow(*row)), nil
}

// ReleaseIdempotencyKey takes a spawner's key back off the failed create holding it and reports
// whether it did, checking the agent's state in the same statement.
func (s *AgentStore) ReleaseIdempotencyKey(ctx context.Context, key string, spawnedBy *string) (bool, error) {
	if key == "" {
		return false, nil
	}

	released, err := s.queries.ReleaseAgentIdempotencyKey(ctx, models.ReleaseAgentIdempotencyKeyParams{
		IdempotencyKey:   &key,
		SpawnedByAgentID: spawnedBy,
	})
	if err != nil {
		return false, fmt.Errorf("failed to release idempotency key %q: %w", key, err)
	}

	return len(released) > 0, nil
}

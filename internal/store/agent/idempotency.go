package agent

import (
	"context"
	"fmt"

	"weave/db"
	"weave/internal/store"
)

// GetByIdempotencyKey returns the live or archived agent recorded under a key.
func (s *AgentStore) GetByIdempotencyKey(ctx context.Context, key string) (*Agent, error) {
	if key == "" {
		return nil, fmt.Errorf("%w: agent for idempotency key %q", store.ErrNotFound, key)
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, &key)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: agent for idempotency key %q", store.ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key, err)
	}

	return s.hydrateMetadataForAgent(ctx, row)
}

// ReleaseIdempotencyKey takes a key back off the failed create holding it and reports whether
// it did, checking the agent's state in the same statement.
func (s *AgentStore) ReleaseIdempotencyKey(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}

	released, err := s.queries.ReleaseAgentIdempotencyKey(ctx, &key)
	if err != nil {
		return false, fmt.Errorf("failed to release idempotency key %q: %w", key, err)
	}

	s.db.TouchAgents(released...)

	return len(released) > 0, nil
}

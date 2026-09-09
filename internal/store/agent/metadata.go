package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"weave/db/models"
	"weave/internal/store"
)

var ErrTooManyMetadataKeys = errors.New("too many metadata keys")

const maxMetadataKeys = 32

type Metadata struct {
	Key   string
	Value string
}

func (s *Store) hydrateMetadataForAgents(ctx context.Context, rows []*models.Agent) ([]Agent, error) {
	agents := make([]Agent, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
		ids = append(ids, row.ID)
	}

	metadata, err := s.metadataByAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range agents {
		agents[i].Metadata = metadata[agents[i].ID]
	}

	return agents, nil
}

func (s *Store) hydrateMetadataForAgent(ctx context.Context, row *models.Agent) (*Agent, error) {
	agents, err := s.hydrateMetadataForAgents(ctx, []*models.Agent{row})
	if err != nil {
		return nil, err
	}

	return &agents[0], nil
}

func (s *Store) metadataByAgent(ctx context.Context, ids []string) (map[string][]Metadata, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentMetadata(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent metadata: %w", err)
	}

	byAgent := make(map[string][]Metadata, len(ids))
	for _, row := range rows {
		byAgent[row.AgentID] = append(byAgent[row.AgentID], Metadata{Key: row.Key, Value: row.Value})
	}

	return byAgent, nil
}

type SetMetadataParams struct {
	ID    string
	Key   string
	Value *string
}

// SetMetadata sets or clears one metadata value and reports whether a row changed, taking an
// archived agent the same as a live one.
func (s *Store) SetMetadata(ctx context.Context, params SetMetadataParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	if params.Value == nil {
		deleted, err := s.queries.DeleteAgentMetadata(ctx, models.DeleteAgentMetadataParams{AgentID: params.ID, Key: params.Key})
		if err != nil {
			return false, fmt.Errorf("failed to clear metadata %q on agent %q: %w", params.Key, params.ID, err)
		}

		return len(deleted) > 0, nil
	}

	beginner, ok := s.db.(txBeginner)
	if !ok {
		return false, fmt.Errorf("agent store has no transaction-capable connection to record metadata %q on agent %q", params.Key, params.ID)
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin metadata transaction for agent %q: %w", params.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := models.New(tx)

	if err := queries.LockAgentMetadata(ctx, params.ID); err != nil {
		return false, fmt.Errorf("failed to lock metadata for agent %q: %w", params.ID, err)
	}

	held, err := queries.ListAgentMetadata(ctx, []string{params.ID})
	if err != nil {
		return false, fmt.Errorf("failed to read metadata on agent %q: %w", params.ID, err)
	}

	revising := slices.ContainsFunc(held, func(row *models.ListAgentMetadataRow) bool {
		return row.Key == params.Key
	})
	if len(held) >= maxMetadataKeys && !revising {
		return false, ErrTooManyMetadataKeys
	}

	written, err := queries.SetAgentMetadata(ctx, models.SetAgentMetadataParams{
		AgentID: params.ID,
		Key:     params.Key,
		Value:   *params.Value,
	})
	if err != nil {
		return false, fmt.Errorf("failed to record metadata %q on agent %q: %w", params.Key, params.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit metadata for agent %q: %w", params.ID, err)
	}

	return len(written) > 0, nil
}

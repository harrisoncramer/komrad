// Package screen keeps the last screen each agent's pane drew at rest.
package screen

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
)

type ScreenStore struct {
	queries *models.Queries
}

// NewScreenStore builds a screen store over db, either the pool or a transaction.
func NewScreenStore(db store.DBTX) *ScreenStore {
	return &ScreenStore{queries: models.New(db)}
}

// Screen is one pane's visible grid as tmux captured it.
type Screen struct {
	AgentID    string
	Frame      string
	Width      int
	Height     int
	CapturedAt time.Time
}

type UpsertParams struct {
	AgentID string
	Frame   string
	Width   int
	Height  int
}

// Upsert replaces the screen kept for an agent.
func (s *ScreenStore) Upsert(ctx context.Context, params UpsertParams) error {
	if !store.IsID(params.AgentID) {
		return fmt.Errorf("failed to save a screen: %q is not an agent id", params.AgentID)
	}

	if err := s.queries.UpsertAgentScreen(ctx, models.UpsertAgentScreenParams{
		AgentID: params.AgentID,
		Frame:   params.Frame,
		Width:   int32(params.Width),
		Height:  int32(params.Height),
	}); err != nil {
		return fmt.Errorf("failed to save the screen of agent %q: %w", params.AgentID, err)
	}

	return nil
}

// Get returns the screen kept for an agent.
func (s *ScreenStore) Get(ctx context.Context, agentID string) (*Screen, error) {
	if !store.IsID(agentID) {
		return nil, fmt.Errorf("%w: screen of agent %q", store.ErrNotFound, agentID)
	}

	row, err := s.queries.GetAgentScreen(ctx, agentID)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: screen of agent %q", store.ErrNotFound, agentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the screen of agent %q: %w", agentID, err)
	}

	return &Screen{
		AgentID:    row.AgentID,
		Frame:      row.Frame,
		Width:      int(row.Width),
		Height:     int(row.Height),
		CapturedAt: row.CapturedAt,
	}, nil
}

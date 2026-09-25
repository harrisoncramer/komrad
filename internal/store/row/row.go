// Package row reads what an agent's row in the panel says: the agent, and the facts drawn into
// it from the tables that are attributes of an agent.
package row

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"

	agentstore "komrad/internal/store/agent"
)

// Row is an agent with the facts about it that live beside its row rather than on it.
type Row struct {
	agentstore.Agent
	FailedRun bool
	// FailedRunLog is the log the unread failure went to.
	FailedRunLog *int32
}

// Page is one page of rows: how many, from where, and whether the archived ones.
type Page struct {
	Limit    int64
	Cursor   *agentstore.Cursor
	Archived bool
}

type RowStore struct {
	queries *models.Queries
	agents  *agentstore.AgentStore
}

// NewRowStore builds a row store over a pool or transaction.
func NewRowStore(db store.DBTX) *RowStore {
	return &RowStore{queries: models.New(db), agents: agentstore.NewAgentStore(db)}
}

// ListPage returns one page of live or archived rows, newest first.
func (s *RowStore) ListPage(ctx context.Context, page Page) ([]Row, error) {
	params := models.ListAgentPageIDsParams{Lim: int32(page.Limit), Archived: page.Archived}
	if page.Cursor != nil {
		params.CursorID = &page.Cursor.ID
	}

	ids, err := s.queries.ListAgentPageIDs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agent rows: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	return s.ListWatched(ctx, ids)
}

// Get returns one agent's row, archived or not, or ErrNotFound when no agent has that id or a delete has claimed it.
func (s *RowStore) Get(ctx context.Context, id string) (*Row, error) {
	rows, err := s.ListWatched(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound.Saying("no agent with id %q", id)
	}

	return &rows[0], nil
}

// attachMetadata builds each watched agent's row with the metadata recorded against it.
func (s *RowStore) attachMetadata(ctx context.Context, watched []*models.ListWatchedAgentsRow) ([]Row, error) {
	if len(watched) == 0 {
		return nil, nil
	}
	ids := xslice.Map(watched, func(row *models.ListWatchedAgentsRow) string { return row.Agent.ID })

	metadata, err := s.agents.ListMetadata(ctx, ids)
	if err != nil {
		return nil, err
	}

	return xslice.Map(watched, func(fact *models.ListWatchedAgentsRow) Row {
		agent := agentstore.FromRow(fact.Agent)
		agent.Metadata = metadata[fact.Agent.ID]

		return Row{
			Agent:        agent,
			FailedRun:    fact.FailedRun,
			FailedRunLog: fact.FailedRunLog,
		}
	}), nil
}

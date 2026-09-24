// Package row reads what an agent's row in the panel says: the agent, and the facts drawn into
// it from the tables that are attributes of an agent.
package row

import (
	"context"
	"fmt"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
)

// Row is an agent with the facts about it that live beside its row rather than on it.
type Row struct {
	agentstore.Agent
	FailedRun bool
	// FailedRunLog is the log the unread failure went to.
	FailedRunLog *int32
	// ReviewVersion changes whenever a thread on the agent's work moves.
	ReviewVersion string
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
	params := models.ListAgentsPageParams{Lim: int32(page.Limit), Archived: page.Archived}
	if page.Cursor != nil {
		params.CursorID = &page.Cursor.ID
	}

	rows, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agent rows: %w", err)
	}

	return s.hydrate(ctx, rows)
}

// List returns the rows of every agent not being deleted, archived ones included, newest first.
func (s *RowStore) List(ctx context.Context) ([]Row, error) {
	rows, err := s.queries.SnapshotAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agent rows: %w", err)
	}

	return s.hydrate(ctx, rows)
}

// ListByKey returns the rows of the given agents that are not being deleted, newest first.
func (s *RowStore) ListByKey(ctx context.Context, ids []string) ([]Row, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentsByKey(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get the rows of %d agents: %w", len(storable), err)
	}

	return s.hydrate(ctx, rows)
}

// Get returns one agent's row, archived or not, or ErrNotFound when no agent has that id.
func (s *RowStore) Get(ctx context.Context, id string) (*Row, error) {
	if !store.IsID(id) {
		return nil, store.ErrNotFound.Saying("no agent with id %q", id)
	}

	row, err := s.queries.GetAgentByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no agent with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the row of agent %q: %w", id, err)
	}

	rows, err := s.hydrate(ctx, []*models.Agent{row})
	if err != nil {
		return nil, err
	}

	return &rows[0], nil
}

// hydrate reads the facts and the metadata drawn into each agent's row.
func (s *RowStore) hydrate(ctx context.Context, agents []*models.Agent) ([]Row, error) {
	if len(agents) == 0 {
		return nil, nil
	}
	ids := xslice.Map(agents, func(agent *models.Agent) string { return agent.ID })

	facts, err := s.queries.ListAgentFacts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the facts of %d agents: %w", len(ids), err)
	}
	factsByID := xslice.KeyBy(facts, func(facts *models.ListAgentFactsRow) string { return facts.ID })

	metadata, err := s.agents.ListMetadata(ctx, ids)
	if err != nil {
		return nil, err
	}

	return xslice.Map(agents, func(agent *models.Agent) Row {
		row := Row{Agent: agentstore.FromRow(*agent)}
		row.Metadata = metadata[agent.ID]
		if facts, ok := factsByID[agent.ID]; ok {
			row.FailedRun = facts.FailedRun
			row.FailedRunLog = facts.FailedRunLog
			row.ReviewVersion = facts.ReviewVersion
		}

		return row
	}), nil
}

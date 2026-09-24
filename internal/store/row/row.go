// Package row reads what an agent's row in the panel says: the agent, and the facts drawn into
// it from the tables that are attributes of an agent.
package row

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
)

// Row is an agent with the facts about it that live beside its row rather than on it.
type Row struct {
	agentstore.Agent
	FailedRun bool
	// FailedRunLog is the log the unread failure went to.
	FailedRunLog *int32
	// LocalRounds is how many rounds of local review the agent's own work has been through.
	LocalRounds int32
	// LocalVerdict is what the last released local review concluded, nil when none has
	// concluded anything.
	LocalVerdict *reviewstore.Verdict
	// LocalReviewSHA is the round that verdict was given on, empty when there is none.
	LocalReviewSHA string
	// RepositoryRead says the sync has read the agent's repository at least once.
	RepositoryRead bool
	// ReviewVersion changes whenever a thread or review on the agent's work moves.
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

	agents, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agent rows: %w", err)
	}
	if len(agents) == 0 {
		return nil, nil
	}

	return s.ListWatched(ctx, xslice.Map(agents, func(agent *models.Agent) string { return agent.ID }))
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

	return xslice.Map(watched, func(watched *models.ListWatchedAgentsRow) Row {
		row := Row{
			Agent:          agentstore.FromRow(watched.Agent),
			FailedRun:      watched.FailedRun,
			FailedRunLog:   watched.FailedRunLog,
			LocalRounds:    watched.LocalRounds,
			RepositoryRead: watched.RepositoryRead,
			ReviewVersion:  watched.ReviewVersion,
		}
		row.Metadata = metadata[watched.Agent.ID]
		if watched.LocalVerdict.Valid {
			row.LocalVerdict = xptr.Ptr(reviewstore.Verdict(watched.LocalVerdict.ReviewVerdict))
		}
		if watched.LocalReviewSha != nil {
			row.LocalReviewSHA = *watched.LocalReviewSha
		}

		return row
	}), nil
}

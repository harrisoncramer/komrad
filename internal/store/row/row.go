// Package row reads what an agent's row in the panel says, the agent and the facts about it
// that other stores hold, in one query.
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
	params := models.ListAgentRowsParams{Lim: int32(page.Limit), Archived: page.Archived}
	if page.Cursor != nil {
		params.CursorID = &page.Cursor.ID
	}

	rows, err := s.queries.ListAgentRows(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agent rows: %w", err)
	}

	return s.hydrateMetadata(ctx, rows)
}

// ListByID returns the live and archived rows of the given agents, newest first.
func (s *RowStore) ListByID(ctx context.Context, ids []string) ([]Row, error) {
	return s.listByID(ctx, ids, false)
}

// ListLiveByID returns the rows of the given agents that the live listing holds, newest first.
func (s *RowStore) ListLiveByID(ctx context.Context, ids []string) ([]Row, error) {
	return s.listByID(ctx, ids, true)
}

func (s *RowStore) listByID(ctx context.Context, ids []string, liveOnly bool) ([]Row, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentRows(ctx, models.ListAgentRowsParams{Ids: storable, LiveOnly: liveOnly, Lim: int32(len(storable))})
	if err != nil {
		return nil, fmt.Errorf("failed to get the rows of %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadata(ctx, rows)
}

func (s *RowStore) hydrateMetadata(ctx context.Context, rows []*models.ListAgentRowsRow) ([]Row, error) {
	listed := xslice.Map(rows, fromRow)

	metadata, err := s.agents.ListMetadata(ctx, xslice.Map(listed, func(row Row) string { return row.ID }))
	if err != nil {
		return nil, err
	}

	for i := range listed {
		listed[i].Metadata = metadata[listed[i].ID]
	}

	return listed, nil
}

func fromRow(row *models.ListAgentRowsRow) Row {
	listed := Row{
		Agent:          agentstore.FromRow(row.Agent),
		FailedRun:      row.FailedRun,
		FailedRunLog:   row.FailedRunLog,
		LocalRounds:    row.LocalRounds,
		RepositoryRead: row.RepositoryRead,
	}
	if row.LocalVerdict.Valid {
		listed.LocalVerdict = xptr.Ptr(reviewstore.Verdict(row.LocalVerdict.ReviewVerdict))
	}
	if row.LocalReviewSha != nil {
		listed.LocalReviewSHA = *row.LocalReviewSha
	}

	return listed
}

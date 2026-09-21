// Package row reads what an agent's row in the panel says, the agent and the facts about it
// that other stores hold, in one query.
package row

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/forge"
	"weave/internal/store"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
)

// Row is an agent with the facts about it that live beside its row rather than on it.
type Row struct {
	agentstore.Agent
	UnreadMail bool
	FailedRun  bool
	// FailedRunLog is the log the unread failure went to.
	FailedRunLog *int32
	// Mark is the letter a person types to jump to this row.
	Mark *string
	// LocalRounds is how many rounds of local review the agent's own work has been through.
	LocalRounds int32
	// LocalVerdict is what the last released local review concluded, nil when none has
	// concluded anything.
	LocalVerdict *reviewstore.Verdict
	// LocalSnapshotSHA is the round that verdict was given on, empty when there is none.
	LocalSnapshotSHA string
	// PullRequest is the one the row stands for, nil when none was opened from the branch.
	PullRequest *pullrequeststore.Summary
	// RepositoryRead says the sync has read the agent's repository at least once.
	RepositoryRead bool
}

// Page is one page of rows: how many, from where, and whether the archived ones.
type Page struct {
	Limit    int64
	Cursor   *agentstore.Cursor
	Archived bool
}

type Store struct {
	queries *models.Queries
	agents  *agentstore.Store
}

// New builds a row store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db), agents: agentstore.New(db)}
}

// ListPage returns one page of live or archived rows, newest first.
func (s *Store) ListPage(ctx context.Context, page Page) ([]Row, error) {
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
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Row, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentRows(ctx, models.ListAgentRowsParams{Ids: storable, Lim: int32(len(storable))})
	if err != nil {
		return nil, fmt.Errorf("failed to get the rows of %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadata(ctx, rows)
}

func (s *Store) hydrateMetadata(ctx context.Context, rows []*models.ListAgentRowsRow) ([]Row, error) {
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
		UnreadMail:     row.UnreadMail,
		FailedRun:      row.FailedRun,
		FailedRunLog:   row.FailedRunLog,
		Mark:           row.Mark,
		LocalRounds:    row.LocalRounds,
		RepositoryRead: row.RepositoryRead,
	}
	if row.LocalVerdict.Valid {
		listed.LocalVerdict = xptr.Ptr(reviewstore.Verdict(row.LocalVerdict.ReviewVerdict))
	}
	if row.LocalSnapshotSha != nil {
		listed.LocalSnapshotSHA = *row.LocalSnapshotSha
	}
	// A number means the branch has a pull request.
	if row.PullRequestNumber != nil {
		listed.PullRequest = &pullrequeststore.Summary{
			Number:   int(*row.PullRequestNumber),
			URL:      *row.PullRequestUrl,
			Title:    *row.PullRequestTitle,
			Draft:    *row.PullRequestDraft,
			MergedAt: row.PullRequestMergedAt,
			ClosedAt: row.PullRequestClosedAt,
		}
		if row.PullRequestReviewDecision != nil {
			listed.PullRequest.ReviewDecision = xptr.Ptr(forge.ReviewDecision(*row.PullRequestReviewDecision))
		}
	}

	return listed
}

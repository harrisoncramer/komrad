package agents

import (
	"context"
	"fmt"

	"komrad/lib/xptr"
	"komrad/lib/xslice"

	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	workflowstore "komrad/internal/store/workflow"
)

type rowPageReader interface {
	ListPage(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListWatched(ctx context.Context, keys []string) ([]rowstore.Row, error)
	Get(ctx context.Context, id string) (*rowstore.Row, error)
}

type watchedLister[R any] interface {
	ListWatched(ctx context.Context, keys []string) ([]R, error)
}

type observedPullRequestLister interface {
	watchedLister[pullrequeststore.PullRequest]
	ListObservations(ctx context.Context, agentIDs []string) ([]pullrequeststore.Observation, error)
}

type agentReviewLister interface {
	watchedLister[reviewstore.Review]
	ListIDsOfAgents(ctx context.Context, agentIDs []string) ([]string, error)
}

// NewRowReaderParams is what a row reader is built over.
type NewRowReaderParams struct {
	Agents       agentGetter
	Rows         rowPageReader
	Mail         watchedLister[mailstore.Mail]
	PullRequests observedPullRequestLister
	Repositories watchedLister[pullrequeststore.Sync]
	Reviews      agentReviewLister
	Workflows    watchedLister[workflowstore.Workflow]
}

// RowReader reads what an agent's row says and what is drawn beside it.
type RowReader struct {
	agents       agentGetter
	rows         rowPageReader
	mail         watchedLister[mailstore.Mail]
	pullRequests observedPullRequestLister
	repositories watchedLister[pullrequeststore.Sync]
	reviews      agentReviewLister
	workflows    watchedLister[workflowstore.Workflow]
}

// NewRowReader builds the reader over the stores.
func NewRowReader(params NewRowReaderParams) *RowReader {
	return &RowReader{
		agents:       params.Agents,
		rows:         params.Rows,
		mail:         params.Mail,
		pullRequests: params.PullRequests,
		repositories: params.Repositories,
		reviews:      params.Reviews,
		workflows:    params.Workflows,
	}
}

// Surroundings are the watched rows a page of agents is drawn beside, the same ones the watch streams.
type Surroundings struct {
	Mail         []mailstore.Mail
	PullRequests []pullrequeststore.PullRequest
	Repositories []pullrequeststore.Sync
	Reviews      []reviewstore.Review
	Workflows    []workflowstore.Workflow
}

// GetAgent returns one agent by id, archived or not.
func (r *RowReader) GetAgent(ctx context.Context, id string) (*agentstore.Agent, error) {
	agent, err := r.agents.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	return agent, nil
}

// ListRows returns one page of agents' rows, newest first.
func (r *RowReader) ListRows(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error) {
	rows, err := r.rows.ListPage(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return rows, nil
}

// ListRowsByID returns the rows of the agents named, leaving out any that are gone.
func (r *RowReader) ListRowsByID(ctx context.Context, ids []string) ([]rowstore.Row, error) {
	rows, err := listWatchedByKey(ctx, r.rows, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the rows of %d agents: %w", len(ids), err)
	}

	return rows, nil
}

// GetRow returns one agent's row by id, archived or not.
func (r *RowReader) GetRow(ctx context.Context, id string) (*rowstore.Row, error) {
	row, err := r.rows.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get the row of agent %q: %w", id, err)
	}

	return row, nil
}

// ListSurroundings reads every unclaimed mail, and the pull requests, repositories, reviews and
// workflows of the agents on the rows.
func (r *RowReader) ListSurroundings(ctx context.Context, rows []rowstore.Row) (*Surroundings, error) {
	agentIDs := xslice.Map(rows, func(row rowstore.Row) string { return row.ID })

	mail, err := r.mail.ListWatched(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list the unclaimed mail: %w", err)
	}

	observations, err := r.pullRequests.ListObservations(ctx, agentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests the agents were seen with: %w", err)
	}

	pullRequests, err := listWatchedByKey(ctx, r.pullRequests, xslice.Map(observations, func(o pullrequeststore.Observation) string {
		return o.PullRequestID
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests: %w", err)
	}

	repositories, err := listWatchedByKey(ctx, r.repositories, xslice.Map(rows, func(row rowstore.Row) string {
		return xptr.Deref(row.RepositoryURL)
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to list the repositories: %w", err)
	}

	reviewIDs, err := r.reviews.ListIDsOfAgents(ctx, agentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of the agents: %w", err)
	}

	reviews, err := listWatchedByKey(ctx, r.reviews, reviewIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews: %w", err)
	}

	workflows, err := listWatchedByKey(ctx, r.workflows, xslice.Map(rows, func(row rowstore.Row) string { return row.WorkflowID }))
	if err != nil {
		return nil, fmt.Errorf("failed to list the workflows: %w", err)
	}

	return &Surroundings{
		Mail:         mail,
		PullRequests: pullRequests,
		Repositories: repositories,
		Reviews:      reviews,
		Workflows:    workflows,
	}, nil
}

// listWatchedByKey reads the watched rows among keys, and nothing when there are none.
func listWatchedByKey[R any](ctx context.Context, lister watchedLister[R], keys []string) ([]R, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	return lister.ListWatched(ctx, keys)
}

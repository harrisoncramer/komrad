package agents

import (
	"context"
	"fmt"

	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
)

const assistantRole = "assistant"

type agentExistsGetter interface {
	agentGetter
	Exists(ctx context.Context, id string) (bool, error)
}

type rowPageReader interface {
	ListPage(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListWatched(ctx context.Context, keys []string) ([]rowstore.Row, error)
	Get(ctx context.Context, id string) (*rowstore.Row, error)
}

type transcriptSearcher interface {
	Get(ctx context.Context, agentID string) (string, error)
	ListLastTurns(ctx context.Context, params transcriptstore.ListLastTurnsParams) ([]transcriptstore.StoredTurn, error)
	Search(ctx context.Context, params transcriptstore.SearchParams) (transcriptstore.Results, error)
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
	Agents       agentExistsGetter
	Rows         rowPageReader
	Transcripts  transcriptSearcher
	Marks        watchedLister[markstore.Mark]
	Mail         watchedLister[mailstore.Mail]
	PullRequests observedPullRequestLister
	Repositories watchedLister[pullrequeststore.Sync]
	Reviews      agentReviewLister
	Workflows    watchedLister[workflowstore.Workflow]
}

// RowReader reads what an agent's row says, what is drawn beside it, and what the agent wrote.
type RowReader struct {
	agents       agentExistsGetter
	rows         rowPageReader
	transcripts  transcriptSearcher
	marks        watchedLister[markstore.Mark]
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
		transcripts:  params.Transcripts,
		marks:        params.Marks,
		mail:         params.Mail,
		pullRequests: params.PullRequests,
		repositories: params.Repositories,
		reviews:      params.Reviews,
		workflows:    params.Workflows,
	}
}

// Surroundings are the watched rows a page of agents is drawn beside, the same ones the watch streams.
type Surroundings struct {
	Marks        []markstore.Mark
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
	rows, err := r.rows.ListWatched(ctx, ids)
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

// ListSurroundings reads every mark and unclaimed mail, and the pull requests, repositories,
// reviews and workflows of the agents on the rows.
func (r *RowReader) ListSurroundings(ctx context.Context, rows []rowstore.Row) (*Surroundings, error) {
	agentIDs := xslice.Map(rows, func(row rowstore.Row) string { return row.ID })

	marks, err := r.marks.ListWatched(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list the marks: %w", err)
	}

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
		Marks:        marks,
		Mail:         mail,
		PullRequests: pullRequests,
		Repositories: repositories,
		Reviews:      reviews,
		Workflows:    workflows,
	}, nil
}

// SearchTranscripts runs a search over the stored transcripts.
func (r *RowReader) SearchTranscripts(ctx context.Context, params transcriptstore.SearchParams) (transcriptstore.Results, error) {
	results, err := r.transcripts.Search(ctx, params)
	if err != nil {
		return transcriptstore.Results{}, fmt.Errorf("failed to search transcripts: %w", err)
	}

	return results, nil
}

// ReadTranscript returns the whole of an agent's stored transcript.
func (r *RowReader) ReadTranscript(ctx context.Context, agentID string) (string, error) {
	if err := r.requireAgent(ctx, agentID); err != nil {
		return "", err
	}

	content, err := r.transcripts.Get(ctx, agentID)
	if err != nil {
		return "", fmt.Errorf("failed to get the transcript of agent %q: %w", agentID, err)
	}

	return content, nil
}

// ListLastTurnsParams names the agent and how many of its own turns to read.
type ListLastTurnsParams struct {
	AgentID string
	Turns   int32
}

// ListLastTurns returns the newest turns an agent wrote and the prompts between them, oldest first, one turn when none is asked for.
func (r *RowReader) ListLastTurns(ctx context.Context, params ListLastTurnsParams) ([]transcriptstore.StoredTurn, error) {
	if err := r.requireAgent(ctx, params.AgentID); err != nil {
		return nil, err
	}

	count := params.Turns
	if count <= 0 {
		count = 1
	}

	turns, err := r.transcripts.ListLastTurns(ctx, transcriptstore.ListLastTurnsParams{
		AgentID: params.AgentID,
		Role:    assistantRole,
		Limit:   count,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the last turns of agent %q: %w", params.AgentID, err)
	}

	return turns, nil
}

func (r *RowReader) requireAgent(ctx context.Context, agentID string) error {
	exists, err := r.agents.Exists(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to look up agent %q: %w", agentID, err)
	}
	if !exists {
		return ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}

	return nil
}

// listWatchedByKey reads the watched rows among keys, and nothing when there are none.
func listWatchedByKey[R any](ctx context.Context, lister watchedLister[R], keys []string) ([]R, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	return lister.ListWatched(ctx, keys)
}

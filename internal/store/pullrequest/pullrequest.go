// Package pullrequest stores the pull requests komrad has read from its forges, the agents each
// was seen with, and where the sync is for every repository and branch it has listed.
package pullrequest

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// PullRequest is one pull request as the forge last described it, owned by nobody.
type PullRequest struct {
	ID string
	// Host is the forge host the repository lives on, such as github.com.
	Host string
	// Forge is which kind of forge the host runs, github or gitlab.
	Forge string
	// ProviderID is the forge's own stable identifier, which with Host is the identity.
	ProviderID string
	// URL is the web address a person opens.
	URL string
	// RepositoryURL is the canonical URL of the repository the pull request targets.
	RepositoryURL string
	// HeadRepositoryURL is the canonical URL of the repository the branch lives in, empty when
	// the forge no longer has it.
	HeadRepositoryURL string
	// Number is the per-repository number a person sees.
	Number     int
	Title      string
	HeadBranch string
	HeadSHA    string
	BaseBranch string
	BaseSHA    string
	Draft      bool
	// State is where the forge says it stands, as OPEN, CLOSED or MERGED.
	State string
	// OpenedAt is when the forge says it was opened.
	OpenedAt time.Time
	// ChangedAt is the forge's last-change stamp.
	ChangedAt   time.Time
	MergedAt    *time.Time
	ClosedAt    *time.Time
	FirstSeenAt time.Time
	// ReviewDecision is where the forge says the reviews stand, nil while it has said nothing.
	ReviewDecision *forge.ReviewDecision
	// DecisionChangedAt is the ChangedAt the decision was read for, nil while it never has been.
	DecisionChangedAt *time.Time
	// Checks is where the forge says the checks on the head stand, nil while it reports none.
	Checks *forge.Checks
	// Mergeability is where the forge says it stands against its base and rules, nil while the
	// forge has not computed it.
	Mergeability *forge.Mergeability
	// ChecksSHA is the head the checks were read for, empty while they never have been.
	ChecksSHA string
	// ChecksReadAt is when the checks were last read, nil while they never have been.
	ChecksReadAt *time.Time
	// LastKnownChecks is the checks as the forge last reported them, empty before it ever has.
	LastKnownChecks string
	// LastKnownMergeability is the mergeability as the forge last reported it, empty before it ever has.
	LastKnownMergeability string
	// LastKnownDecision is the review decision as the forge last reported it, empty before it ever has.
	LastKnownDecision string
}

// IsFinished says whether the pull request merged or closed.
func (p PullRequest) IsFinished() bool {
	return p.MergedAt != nil || p.ClosedAt != nil
}

// UpsertParams is a pull request as the forge describes it, which is every column but the id
// and when komrad first saw it.
type UpsertParams struct {
	Host              string
	Forge             string
	ProviderID        string
	URL               string
	RepositoryURL     string
	HeadRepositoryURL string
	Number            int
	Title             string
	HeadBranch        string
	HeadSHA           string
	BaseBranch        string
	BaseSHA           string
	Draft             bool
	State             string
	OpenedAt          time.Time
	ChangedAt         time.Time
	MergedAt          *time.Time
	ClosedAt          *time.Time
}

// Outcome is what an upsert did to the row.
type Outcome struct {
	ID string
	// Inserted says the row is new.
	Inserted bool
	// Changed says the row was written, whether new or different from what was stored.
	Changed bool
	// OldState is the state the row held before a change, nil when it is new or unchanged.
	OldState *string
}

// StandingChange is the last known standing before and after a write, a blank from the forge
// leaving the last known value as it was.
type StandingChange struct {
	OldChecks         *string
	OldChecksSHA      *string
	OldMergeability   *string
	OldDecision       *string
	KnownChecks       *string
	KnownChecksSHA    *string
	KnownMergeability *string
	KnownDecision     *string
}

// Observation is one agent having been seen with one pull request.
type Observation struct {
	AgentID       string
	PullRequestID string
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
}

// PullRequestStore reads and writes pull requests and their observations.
type PullRequestStore struct {
	queries *models.Queries
}

// NewPullRequestStore builds a pull request store over a pool or transaction.
func NewPullRequestStore(db store.DBTX) *PullRequestStore {
	return &PullRequestStore{queries: models.New(db)}
}

// Upsert writes a pull request on its identity, leaving a row whose forge-owned columns already
// match untouched, and says what it did.
func (s *PullRequestStore) Upsert(ctx context.Context, params UpsertParams) (*Outcome, error) {
	row, err := s.queries.UpsertPullRequest(ctx, models.UpsertPullRequestParams{
		Host:              params.Host,
		Forge:             params.Forge,
		ProviderID:        params.ProviderID,
		Url:               params.URL,
		RepositoryUrl:     params.RepositoryURL,
		HeadRepositoryUrl: params.HeadRepositoryURL,
		Number:            int32(params.Number),
		Title:             params.Title,
		HeadBranch:        params.HeadBranch,
		HeadSha:           params.HeadSHA,
		BaseBranch:        params.BaseBranch,
		BaseSha:           params.BaseSHA,
		Draft:             params.Draft,
		State:             params.State,
		OpenedAt:          params.OpenedAt,
		ChangedAt:         params.ChangedAt,
		MergedAt:          params.MergedAt,
		ClosedAt:          params.ClosedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write pull request %s on %s: %w", params.ProviderID, params.Host, err)
	}

	return &Outcome{
		ID:       row.ID,
		Inserted: row.Inserted,
		Changed:  row.Changed,
		OldState: row.OldState,
	}, nil
}

// GetByIdentity is the pull request a forge host knows by an identifier, or ErrNotFound.
func (s *PullRequestStore) GetByIdentity(ctx context.Context, host, providerID string) (*PullRequest, error) {
	row, err := s.queries.GetPullRequestByIdentity(ctx, models.GetPullRequestByIdentityParams{Host: host, ProviderID: providerID})
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no pull request %s on %s", providerID, host)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pull request %s on %s: %w", providerID, host, err)
	}

	pullRequest := fromRow(row)

	return &pullRequest, nil
}

// Cursor is where a page of pull requests left off: whether the last was finished, when it
// was opened and its id.
type Cursor struct {
	Finished bool      `json:"finished"`
	OpenedAt time.Time `json:"opened_at"`
	ID       string    `json:"id"`
}

// Validate refuses a cursor that names no row.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPageParams narrows a page to one repository and, when set, one branch.
type ListPageParams struct {
	RepositoryURL string
	HeadBranch    string
	Limit         int32
	Cursor        *Cursor
}

// ListPage is one page of a repository's stored pull requests, the unfinished ones first and
// each group newest opened first.
func (s *PullRequestStore) ListPage(ctx context.Context, params ListPageParams) ([]PullRequest, error) {
	query := models.ListPullRequestsPageParams{
		RepositoryUrl: params.RepositoryURL,
		HeadBranch:    params.HeadBranch,
		Lim:           params.Limit,
	}
	if params.Cursor != nil {
		query.CursorID = &params.Cursor.ID
		query.CursorFinished = params.Cursor.Finished
		query.CursorOpenedAt = params.Cursor.OpenedAt
	}

	rows, err := s.queries.ListPullRequestsPage(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of the pull requests of %s: %w", params.RepositoryURL, err)
	}

	return xslice.Map(rows, fromRow), nil
}

// SetStanding records where the forge said a pull request's reviews, checks and mergeability
// stand, stamps the read at readAt whether or not anything moved, and answers the last known
// standing before and after.
func (s *PullRequestStore) SetStanding(ctx context.Context, id string, standing forge.Standing, readAt time.Time) (*StandingChange, error) {
	var decision *string
	if standing.Decision != forge.ReviewDecisionNone {
		decision = xptr.Ptr(string(standing.Decision))
	}
	var checks *string
	if standing.Checks != forge.ChecksNone {
		checks = xptr.Ptr(string(standing.Checks))
	}
	var mergeability *string
	if standing.Mergeability != forge.MergeabilityUnknown {
		mergeability = xptr.Ptr(string(standing.Mergeability))
	}

	row, err := s.queries.SetStanding(ctx, models.SetStandingParams{
		ID:             id,
		ReviewDecision: decision,
		Checks:         checks,
		Mergeability:   mergeability,
		ChecksSha:      standing.HeadSHA,
		ChecksReadAt:   readAt,
	})
	if db.IsNoRows(err) {
		return &StandingChange{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to record the standing of pull request %s: %w", id, err)
	}

	return &StandingChange{
		OldChecks:         row.OldChecks,
		OldChecksSHA:      row.OldChecksSha,
		OldMergeability:   row.OldMergeability,
		OldDecision:       row.OldDecision,
		KnownChecks:       row.KnownChecks,
		KnownChecksSHA:    row.KnownChecksSha,
		KnownMergeability: row.KnownMergeability,
		KnownDecision:     row.KnownDecision,
	}, nil
}

// Observe records every agent not being deleted beside every stored pull request opened from
// its branch in its repository, inserting a pair not seen before and moving last_seen_at on one
// seen more than an hour ago, and answers how many rows it wrote.
func (s *PullRequestStore) Observe(ctx context.Context) (int64, error) {
	written, err := s.queries.ObserveAgentPullRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to observe agents beside their pull requests: %w", err)
	}

	return written, nil
}

// ListObservations is every pull request any of the given agents has been seen with, oldest first.
func (s *PullRequestStore) ListObservations(ctx context.Context, agentIDs []string) ([]Observation, error) {
	storable := store.KeepIDs(agentIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentPullRequests(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests %d agents were seen with: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.AgentPullRequest) Observation {
		return Observation{
			AgentID:       row.AgentID,
			PullRequestID: row.PullRequestID,
			FirstSeenAt:   row.FirstSeenAt,
			LastSeenAt:    row.LastSeenAt,
		}
	}), nil
}

// ListLiveRepositories is every repository a live agent sits on.
func (s *PullRequestStore) ListLiveRepositories(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListLiveRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the repositories live agents sit on: %w", err)
	}

	repositories := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			repositories = append(repositories, *row)
		}
	}

	return repositories, nil
}

func fromRow(row *models.PullRequest) PullRequest {
	return PullRequest{
		ID:                    row.ID,
		Host:                  row.Host,
		Forge:                 row.Forge,
		ProviderID:            row.ProviderID,
		URL:                   row.Url,
		RepositoryURL:         row.RepositoryUrl,
		HeadRepositoryURL:     row.HeadRepositoryUrl,
		Number:                int(row.Number),
		Title:                 row.Title,
		HeadBranch:            row.HeadBranch,
		HeadSHA:               row.HeadSha,
		BaseBranch:            row.BaseBranch,
		BaseSHA:               row.BaseSha,
		Draft:                 row.Draft,
		State:                 row.State,
		OpenedAt:              row.OpenedAt,
		ChangedAt:             row.ChangedAt,
		MergedAt:              row.MergedAt,
		ClosedAt:              row.ClosedAt,
		FirstSeenAt:           row.FirstSeenAt,
		ReviewDecision:        storedReviewDecision(row.ReviewDecision),
		DecisionChangedAt:     row.DecisionChangedAt,
		Checks:                StoredChecks(row.Checks),
		Mergeability:          StoredMergeability(row.Mergeability),
		ChecksSHA:             xptr.Deref(row.ChecksSha),
		ChecksReadAt:          row.ChecksReadAt,
		LastKnownChecks:       xptr.Deref(row.LastKnownChecks),
		LastKnownMergeability: xptr.Deref(row.LastKnownMergeability),
		LastKnownDecision:     xptr.Deref(row.LastKnownDecision),
	}
}

// StoredMergeability is the mergeability column as the forge's word, nil for a null.
func StoredMergeability(stored *string) *forge.Mergeability {
	if stored == nil {
		return nil
	}

	return xptr.Ptr(forge.Mergeability(*stored))
}

// StoredChecks is the checks column as the forge's word, nil for a null.
func StoredChecks(stored *string) *forge.Checks {
	if stored == nil {
		return nil
	}

	return xptr.Ptr(forge.Checks(*stored))
}

func storedReviewDecision(stored *string) *forge.ReviewDecision {
	if stored == nil {
		return nil
	}

	return xptr.Ptr(forge.ReviewDecision(*stored))
}

// GetByID is one stored pull request, or ErrNotFound.
func (s *PullRequestStore) GetByID(ctx context.Context, id string) (*PullRequest, error) {
	if !store.IsID(id) {
		return nil, store.ErrNotFound.Saying("no pull request %s", id)
	}

	row, err := s.queries.GetPullRequest(ctx, id)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no pull request %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pull request %s: %w", id, err)
	}

	pullRequest := fromRow(row)

	return &pullRequest, nil
}

// ListRecipients is every live agent, children aside, whose checkout is on a pull request's
// branch in its repository, in id order.
func (s *PullRequestStore) ListRecipients(ctx context.Context, id string) ([]string, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	ids, err := s.queries.ListPullRequestRecipients(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents on pull request %s: %w", id, err)
	}

	return ids, nil
}

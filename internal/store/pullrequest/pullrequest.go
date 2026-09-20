// Package pullrequest stores the pull requests weave has read from its forges, the agents each
// was seen with, and where the sync is for every repository and branch it has listed.
package pullrequest

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/forge"
	"weave/internal/store"
	"weave/lib/xptr"
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
	// State is the forge's own word, kept verbatim.
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
}

// IsFinished says whether the pull request merged or closed.
func (p PullRequest) IsFinished() bool {
	return p.MergedAt != nil || p.ClosedAt != nil
}

// Current is the pull request an agent's row stands for: the newest opened from its branch that
// is not finished, else the newest opened of the finished.
type Current struct {
	AgentID     string
	PullRequest PullRequest
}

// UpsertParams is a pull request as the forge describes it, which is every column but the id
// and when weave first saw it.
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
}

// Observation is one agent having been seen with one pull request.
type Observation struct {
	AgentID       string
	PullRequestID string
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
}

// Sync is where the sync is for one repository.
type Sync struct {
	RepositoryURL string
	// UpdatedThrough is the newest forge changed_at the last completed sweep covered.
	UpdatedThrough *time.Time
	// ETag is what page one answered on the last completed sweep.
	ETag string
	// ValidatedAt is the last time page one was read without an ETag and answered with a body.
	ValidatedAt *time.Time
	// SweepNextPage is the next page a sweep in progress has not yet written, nil between sweeps.
	SweepNextPage *int
	// SweepNewest is the changed_at of the first pull request on page one of the sweep in progress.
	SweepNewest *time.Time
	// SweepETag is what page one answered for the sweep in progress.
	SweepETag *string
	// ReadAt is the last time any request was made for the repository.
	ReadAt *time.Time
	// Calls is how many requests the most recent pass made for the repository.
	Calls int
}

// IsSweeping says whether a sweep is in progress.
func (s Sync) IsSweeping() bool {
	return s.SweepNextPage != nil
}

// BranchScan is where the one-time listing of a branch's pull requests is.
type BranchScan struct {
	RepositoryURL string
	Branch        string
	// NextPage is the next page not yet written.
	NextPage int
	// CompletedAt is when the listing answered its last page, nil while it has not.
	CompletedAt *time.Time
}

type Store struct {
	queries *models.Queries
}

// New builds a pull request store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// Upsert writes a pull request on its identity, leaving a row whose forge-owned columns already
// match untouched, and says what it did.
func (s *Store) Upsert(ctx context.Context, params UpsertParams) (*Outcome, error) {
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
	}, nil
}

// GetByIdentity is the pull request a forge host knows by an identifier, or ErrNotFound.
func (s *Store) GetByIdentity(ctx context.Context, host, providerID string) (*PullRequest, error) {
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

// ListOnBranch is every pull request opened from a branch of a repository, newest first by the
// forge's creation time.
func (s *Store) ListOnBranch(ctx context.Context, repositoryURL, branch string) ([]PullRequest, error) {
	rows, err := s.queries.ListPullRequestsOnBranch(ctx, models.ListPullRequestsOnBranchParams{RepositoryUrl: repositoryURL, HeadBranch: branch})
	if err != nil {
		return nil, fmt.Errorf("failed to list pull requests on %s of %s: %w", branch, repositoryURL, err)
	}

	pullRequests := make([]PullRequest, 0, len(rows))
	for _, row := range rows {
		pullRequests = append(pullRequests, fromRow(row))
	}

	return pullRequests, nil
}

// ListForAgent is every pull request opened from an agent's branch in its repository, newest
// opened first, on the same terms the agent's row is attributed by.
func (s *Store) ListForAgent(ctx context.Context, agentID string) ([]PullRequest, error) {
	if !store.IsID(agentID) {
		return nil, store.ErrNotFound.Saying("no agent with id %q", agentID)
	}

	rows, err := s.queries.ListAgentBranchPullRequests(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests of agent %q: %w", agentID, err)
	}

	pullRequests := make([]PullRequest, 0, len(rows))
	for _, row := range rows {
		pullRequests = append(pullRequests, fromRow(row))
	}

	return pullRequests, nil
}

// ListStaleDecisions is every open pull request on a branch a live agent sits on in a
// repository whose review decision has not been read since it last changed, newest change
// first.
func (s *Store) ListStaleDecisions(ctx context.Context, repositoryURL string) ([]PullRequest, error) {
	rows, err := s.queries.ListStaleDecisions(ctx, repositoryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list the stale review decisions of %s: %w", repositoryURL, err)
	}

	pullRequests := make([]PullRequest, 0, len(rows))
	for _, row := range rows {
		pullRequests = append(pullRequests, fromRow(row))
	}

	return pullRequests, nil
}

// SetReviewDecision records where the forge said a pull request's reviews stand, read for the
// change the row holds now.
func (s *Store) SetReviewDecision(ctx context.Context, id string, decision forge.ReviewDecision) error {
	var stored *string
	if decision != forge.ReviewDecisionNone {
		stored = xptr.Ptr(string(decision))
	}

	err := s.queries.SetReviewDecision(ctx, models.SetReviewDecisionParams{ID: id, ReviewDecision: stored})
	if err != nil {
		return fmt.Errorf("failed to record the review decision of pull request %s: %w", id, err)
	}

	return nil
}

// ListCurrentPullRequests is the pull request each live agent's row stands for, leaving out the
// agents whose branch has none.
func (s *Store) ListCurrentPullRequests(ctx context.Context) ([]Current, error) {
	rows, err := s.queries.ListCurrentPullRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the current pull requests: %w", err)
	}

	current := make([]Current, 0, len(rows))
	for _, row := range rows {
		current = append(current, Current{AgentID: row.AgentID, PullRequest: fromRow(&row.PullRequest)})
	}

	return current, nil
}

// ListReadRepositories is every repository the sync has made at least one request for.
func (s *Store) ListReadRepositories(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListReadRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the repositories the sync has read: %w", err)
	}

	return rows, nil
}

// Observe records every live agent beside every stored pull request opened from its branch in
// its repository, inserting a pair not seen before and moving last_seen_at on one seen more than
// an hour ago, and answers how many rows it wrote.
func (s *Store) Observe(ctx context.Context) (int64, error) {
	written, err := s.queries.ObserveAgentPullRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to observe agents beside their pull requests: %w", err)
	}

	return written, nil
}

// ListObservations is every pull request an agent has been seen with, oldest first.
func (s *Store) ListObservations(ctx context.Context, agentID string) ([]Observation, error) {
	if !store.IsID(agentID) {
		return nil, store.ErrNotFound.Saying("no agent with id %q", agentID)
	}

	rows, err := s.queries.ListAgentPullRequests(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests agent %q was seen with: %w", agentID, err)
	}

	observations := make([]Observation, 0, len(rows))
	for _, row := range rows {
		observations = append(observations, Observation{
			AgentID:       row.AgentID,
			PullRequestID: row.PullRequestID,
			FirstSeenAt:   row.FirstSeenAt,
			LastSeenAt:    row.LastSeenAt,
		})
	}

	return observations, nil
}

// ListLiveRepositories is every repository a live agent sits on.
func (s *Store) ListLiveRepositories(ctx context.Context) ([]string, error) {
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

// ListUnscannedBranches is every branch a live agent sits on in a repository whose one-time
// listing has not completed.
func (s *Store) ListUnscannedBranches(ctx context.Context, repositoryURL string) ([]string, error) {
	rows, err := s.queries.ListUnscannedBranches(ctx, &repositoryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list the unscanned branches of %s: %w", repositoryURL, err)
	}

	branches := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			branches = append(branches, *row)
		}
	}

	return branches, nil
}

// GetBranchScan is where the listing of a branch is, or ErrNotFound when it has not started.
func (s *Store) GetBranchScan(ctx context.Context, repositoryURL, branch string) (*BranchScan, error) {
	row, err := s.queries.GetBranchScan(ctx, models.GetBranchScanParams{RepositoryUrl: repositoryURL, Branch: branch})
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("%s of %s has not been scanned", branch, repositoryURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the scan of %s of %s: %w", branch, repositoryURL, err)
	}

	return &BranchScan{
		RepositoryURL: row.RepositoryUrl,
		Branch:        row.Branch,
		NextPage:      int(row.NextPage),
		CompletedAt:   row.CompletedAt,
	}, nil
}

// SetBranchScanNextPage records the next page of a branch's listing still to write, starting
// the scan when it has not.
func (s *Store) SetBranchScanNextPage(ctx context.Context, repositoryURL, branch string, nextPage int) error {
	err := s.queries.SetBranchScanNextPage(ctx, models.SetBranchScanNextPageParams{
		RepositoryUrl: repositoryURL,
		Branch:        branch,
		NextPage:      int32(nextPage),
	})
	if err != nil {
		return fmt.Errorf("failed to advance the scan of %s of %s: %w", branch, repositoryURL, err)
	}

	return nil
}

// CompleteBranchScan records that a branch's listing answered its last page.
func (s *Store) CompleteBranchScan(ctx context.Context, repositoryURL, branch string, nextPage int) error {
	err := s.queries.CompleteBranchScan(ctx, models.CompleteBranchScanParams{
		RepositoryUrl: repositoryURL,
		Branch:        branch,
		NextPage:      int32(nextPage),
	})
	if err != nil {
		return fmt.Errorf("failed to complete the scan of %s of %s: %w", branch, repositoryURL, err)
	}

	return nil
}

// EnsureSync makes sure a repository has a sync row, leaving one it has alone.
func (s *Store) EnsureSync(ctx context.Context, repositoryURL string) error {
	if err := s.queries.EnsurePullRequestSync(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to ensure the sync row for %s: %w", repositoryURL, err)
	}

	return nil
}

// GetSync is where the sync is for a repository, or ErrNotFound.
func (s *Store) GetSync(ctx context.Context, repositoryURL string) (*Sync, error) {
	row, err := s.queries.GetPullRequestSync(ctx, repositoryURL)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("%s has no sync row", repositoryURL)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the sync row for %s: %w", repositoryURL, err)
	}

	sync := Sync{
		RepositoryURL:  row.RepositoryUrl,
		UpdatedThrough: row.UpdatedThrough,
		ETag:           row.Etag,
		ValidatedAt:    row.ValidatedAt,
		SweepNewest:    row.SweepNewest,
		SweepETag:      row.SweepEtag,
		ReadAt:         row.ReadAt,
		Calls:          int(row.Calls),
	}
	if row.SweepNextPage != nil {
		next := int(*row.SweepNextPage)
		sync.SweepNextPage = &next
	}

	return &sync, nil
}

// ResetSyncCalls starts a pass's count of requests for a repository at zero.
func (s *Store) ResetSyncCalls(ctx context.Context, repositoryURL string) error {
	if err := s.queries.ResetPullRequestSyncCalls(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to reset the call count for %s: %w", repositoryURL, err)
	}

	return nil
}

// RecordSyncRequest counts one request made for a repository and stamps when.
func (s *Store) RecordSyncRequest(ctx context.Context, repositoryURL string) error {
	if err := s.queries.RecordPullRequestSyncRequest(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to count a request for %s: %w", repositoryURL, err)
	}

	return nil
}

// StartSweepParams is what page one of a sweep answered with.
type StartSweepParams struct {
	RepositoryURL string
	// Newest is the changed_at of the first pull request on page one.
	Newest time.Time
	// ETag is what page one answered.
	ETag string
	// ValidatedAt is when page one was read without an ETag and answered with a body, nil when
	// it was read with one.
	ValidatedAt *time.Time
}

// StartSweep records a sweep beginning at page one.
func (s *Store) StartSweep(ctx context.Context, params StartSweepParams) error {
	newest := params.Newest
	etag := params.ETag
	err := s.queries.StartPullRequestSweep(ctx, models.StartPullRequestSweepParams{
		RepositoryUrl: params.RepositoryURL,
		SweepNewest:   &newest,
		SweepEtag:     &etag,
		ValidatedAt:   params.ValidatedAt,
	})
	if err != nil {
		return fmt.Errorf("failed to start a sweep of %s: %w", params.RepositoryURL, err)
	}

	return nil
}

// AdvanceSweep records the next page of a sweep still to write.
func (s *Store) AdvanceSweep(ctx context.Context, repositoryURL string, nextPage int) error {
	next := int32(nextPage)
	err := s.queries.AdvancePullRequestSweep(ctx, models.AdvancePullRequestSweepParams{
		RepositoryUrl: repositoryURL,
		SweepNextPage: &next,
	})
	if err != nil {
		return fmt.Errorf("failed to advance the sweep of %s: %w", repositoryURL, err)
	}

	return nil
}

// CompleteSweep promotes the sweep in progress: the high-water mark moves to its newest stamp
// unless the mark is already newer, its ETag becomes the repository's, and the sweep columns
// clear.
func (s *Store) CompleteSweep(ctx context.Context, repositoryURL string) error {
	if err := s.queries.CompletePullRequestSweep(ctx, repositoryURL); err != nil {
		return fmt.Errorf("failed to complete the sweep of %s: %w", repositoryURL, err)
	}

	return nil
}

func fromRow(row *models.PullRequest) PullRequest {
	return PullRequest{
		ID:                row.ID,
		Host:              row.Host,
		Forge:             row.Forge,
		ProviderID:        row.ProviderID,
		URL:               row.Url,
		RepositoryURL:     row.RepositoryUrl,
		HeadRepositoryURL: row.HeadRepositoryUrl,
		Number:            int(row.Number),
		Title:             row.Title,
		HeadBranch:        row.HeadBranch,
		HeadSHA:           row.HeadSha,
		BaseBranch:        row.BaseBranch,
		BaseSHA:           row.BaseSha,
		Draft:             row.Draft,
		State:             row.State,
		OpenedAt:          row.OpenedAt,
		ChangedAt:         row.ChangedAt,
		MergedAt:          row.MergedAt,
		ClosedAt:          row.ClosedAt,
		FirstSeenAt:       row.FirstSeenAt,
		ReviewDecision:    reviewDecisionOf(row.ReviewDecision),
		DecisionChangedAt: row.DecisionChangedAt,
	}
}

func reviewDecisionOf(stored *string) *forge.ReviewDecision {
	if stored == nil {
		return nil
	}

	return xptr.Ptr(forge.ReviewDecision(*stored))
}

// Package pullrequest stores the pull requests komrad has read from its forges, the agents each
// was seen with, and which repositories the sync has read.
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
	Author     string
	Body       string
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
	// MergeReadiness is what komrad remembers of where the forge said the reviews, checks,
	// mergeability and auto-merge stand.
	MergeReadiness forge.RememberedMergeReadiness
	// HeadStatusChecks is every status check stored on the head, or on the latest read head when the head was never read.
	HeadStatusChecks []forge.StatusCheck
}

// IsFinished says whether the pull request merged or closed.
func (p PullRequest) IsFinished() bool {
	return p.MergedAt != nil || p.ClosedAt != nil
}

// outcome is what an upsert did to the row.
type outcome struct {
	ID string
	// Inserted says the row is new.
	Inserted bool
	// Changed says the row was written, whether new or different from what was stored.
	Changed bool
	// OldState is the state the row held before a change, nil when it is new or unchanged.
	OldState *string
}

// MergeReadinessChange is what komrad remembers of a pull request's merge readiness before and after one read.
type MergeReadinessChange struct {
	Before forge.RememberedMergeReadiness
	After  forge.RememberedMergeReadiness
	// HeadStatusChecks is every status check stored on the head after the read, or on the latest read head when the head
	// was never read.
	HeadStatusChecks []forge.StatusCheck
	// NewFailure is whether the read found a failed check on the latest read head that the reading before it
	// did not hold, every failure on a head read for the first time counting as new.
	NewFailure bool
}

// Observation is one task having been seen with one pull request.
type Observation struct {
	TaskID        string
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
	Author            string
	Body              string
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

// Upsert writes a pull request on its identity, leaving a row whose forge-owned columns already
// match untouched, and says what it did.
func (s *PullRequestStore) Upsert(ctx context.Context, params UpsertParams) (*outcome, error) {
	row, err := s.queries.UpsertPullRequest(ctx, models.UpsertPullRequestParams{
		Host:              params.Host,
		Forge:             params.Forge,
		ProviderID:        params.ProviderID,
		Url:               params.URL,
		RepositoryUrl:     params.RepositoryURL,
		HeadRepositoryUrl: params.HeadRepositoryURL,
		Number:            int32(params.Number),
		Title:             params.Title,
		Author:            params.Author,
		Body:              params.Body,
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

	return &outcome{
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

	return s.withOneMergeReadiness(ctx, row)
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

	return s.withMergeReadiness(ctx, rows)
}

// SetMergeReadinessParams is one read of a pull request's merge readiness.
type SetMergeReadinessParams struct {
	ID             string
	MergeReadiness forge.MergeReadiness
	// HeadSHA is the head the merge readiness was read for.
	HeadSHA string
	ReadAt  time.Time
}

// SetMergeReadiness records one read of a pull request's merge readiness and must run on a store built inside InTx.
func (s *PullRequestStore) SetMergeReadiness(ctx context.Context, params SetMergeReadinessParams) (*MergeReadinessChange, error) {
	row, err := s.queries.GetPullRequestForUpdate(ctx, params.ID)
	if db.IsNoRows(err) {
		return &MergeReadinessChange{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lock pull request %s to record its merge readiness: %w", params.ID, err)
	}

	readBefore, err := headReadings(ctx, s.queries, []string{params.ID})
	if err != nil {
		return nil, err
	}
	before := rememberedMergeReadinessOf(row, readBefore[params.ID])

	if listing := params.MergeReadiness.Listing; listing != nil {
		if err := setStatusChecks(ctx, s.queries, SetStatusChecksParams{
			PullRequestID: params.ID,
			HeadSHA:       params.HeadSHA,
			StatusChecks:  listing.StatusChecks,
			Truncated:     listing.Truncated,
			ReadAt:        params.ReadAt,
		}); err != nil {
			return nil, err
		}
	}

	readAfter, err := headReadings(ctx, s.queries, []string{params.ID})
	if err != nil {
		return nil, err
	}
	after := before.RememberRead(params.MergeReadiness, params.HeadSHA, params.ReadAt)
	after.Checks = rememberedChecksOf(readAfter[params.ID])
	if err := s.queries.WriteMergeReadiness(ctx, toWriteMergeReadiness(params.ID, after)); err != nil {
		return nil, fmt.Errorf("failed to record the merge readiness of pull request %s: %w", params.ID, err)
	}

	return &MergeReadinessChange{
		Before:           before,
		After:            after,
		HeadStatusChecks: readAfter[params.ID].StatusChecks,
		NewFailure:       hasNewFailure(readBefore[params.ID], readAfter[params.ID]),
	}, nil
}

// withOneMergeReadiness is the row as a pull request, its checks rolled up from the latest reading of them.
func (s *PullRequestStore) withOneMergeReadiness(ctx context.Context, row *models.PullRequest) (*PullRequest, error) {
	pullRequests, err := s.withMergeReadiness(ctx, []*models.PullRequest{row})
	if err != nil {
		return nil, err
	}

	return &pullRequests[0], nil
}

// withMergeReadiness is each row as a pull request, its checks rolled up from the latest reading of them.
func (s *PullRequestStore) withMergeReadiness(ctx context.Context, rows []*models.PullRequest) ([]PullRequest, error) {
	readings, err := headReadings(ctx, s.queries, xslice.Map(rows, func(row *models.PullRequest) string { return row.ID }))
	if err != nil {
		return nil, err
	}

	return xslice.Map(rows, func(row *models.PullRequest) PullRequest { return fromRow(row, readings[row.ID]) }), nil
}

// Observe records every task beside every stored pull request opened from its checkout's branch
// in its repository, inserting a pair not seen before and moving last_seen_at on one
// seen more than an hour ago, and answers how many rows it wrote.
func (s *PullRequestStore) Observe(ctx context.Context) (int64, error) {
	written, err := s.queries.ObserveTaskPullRequests(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to observe tasks beside their pull requests: %w", err)
	}

	return written, nil
}

// ListObservations returns every pull request any of the given tasks has been seen with, oldest first.
func (s *PullRequestStore) ListObservations(ctx context.Context, taskIDs []string) ([]Observation, error) {
	storable := store.KeepIDs(taskIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListTaskPullRequests(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests %d tasks were seen with: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.TaskPullRequest) Observation {
		return Observation{
			TaskID:        row.TaskID,
			PullRequestID: row.PullRequestID,
			FirstSeenAt:   row.FirstSeenAt,
			LastSeenAt:    row.LastSeenAt,
		}
	}), nil
}

// ListLiveRepositories is every repository a live task's checkout sits on.
func (s *PullRequestStore) ListLiveRepositories(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListLiveRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the repositories live tasks sit on: %w", err)
	}

	repositories := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			repositories = append(repositories, *row)
		}
	}

	return repositories, nil
}

func fromRow(row *models.PullRequest, checks StatusCheckReading) PullRequest {
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
		Author:            row.Author,
		Body:              row.Body,
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
		MergeReadiness:    rememberedMergeReadinessOf(row, checks),
		HeadStatusChecks:  checks.StatusChecks,
	}
}

func rememberedMergeReadinessOf(row *models.PullRequest, checks StatusCheckReading) forge.RememberedMergeReadiness {
	wasRead := row.LatestHeadSha != nil
	readiness := forge.RememberedMergeReadiness{
		Decision: forge.RememberedAnswer[forge.ReviewDecision]{
			Value:              forge.ReviewDecision(xptr.Deref(row.KnownDecision)),
			WasLatestReadBlank: wasRead && row.LatestDecision == nil,
		},
		Checks: rememberedChecksOf(checks),
		Mergeability: forge.RememberedAnswer[forge.Mergeability]{
			Value:              forge.Mergeability(xptr.Deref(row.KnownMergeability)),
			HeadSHA:            xptr.Deref(row.KnownMergeabilitySha),
			WasLatestReadBlank: wasRead && row.LatestMergeability == nil,
		},
		LatestReadHeadSHA:      xptr.Deref(row.LatestHeadSha),
		MergeabilityBlankSince: row.MergeabilityBlankSince,
	}
	if row.AutoMerge != nil {
		readiness.AutoMerge = &forge.AutoMerge{On: *row.AutoMerge, Allowed: row.AutoMergeable}
	}

	return readiness
}

func toWriteMergeReadiness(id string, readiness forge.RememberedMergeReadiness) models.WriteMergeReadinessParams {
	params := models.WriteMergeReadinessParams{
		ID:                     id,
		LatestDecision:         latestReadOf(readiness.Decision),
		LatestMergeability:     latestReadOf(readiness.Mergeability),
		LatestHeadSha:          &readiness.LatestReadHeadSHA,
		KnownMergeability:      xptr.NonEmpty(string(readiness.Mergeability.Value)),
		KnownMergeabilitySha:   xptr.NonEmpty(readiness.Mergeability.HeadSHA),
		MergeabilityBlankSince: readiness.MergeabilityBlankSince,
		KnownDecision:          xptr.NonEmpty(string(readiness.Decision.Value)),
	}
	if readiness.AutoMerge != nil {
		params.AutoMerge = &readiness.AutoMerge.On
		params.AutoMergeable = readiness.AutoMerge.Allowed
	}

	return params
}

func latestReadOf[T ~string](answer forge.RememberedAnswer[T]) *string {
	if answer.WasLatestReadBlank {
		return nil
	}

	return xptr.NonEmpty(string(answer.Value))
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

	return s.withOneMergeReadiness(ctx, row)
}

// ListRecipients is the live writer of each task whose checkout is on a pull request's branch in its
// repository, in agent id order.
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

// GetParentPullRequestParams names the parent's repository and branch, and when the child took that parent.
type GetParentPullRequestParams struct {
	RepositoryURL string
	HeadBranch    string
	Since         time.Time
}

// GetParentPullRequest is a child's parent pull request on a branch: the earliest one open, or opened since the child took that parent.
func (s *PullRequestStore) GetParentPullRequest(ctx context.Context, params GetParentPullRequestParams) (*PullRequest, error) {
	row, err := s.queries.GetParentPullRequest(ctx, models.GetParentPullRequestParams{
		RepositoryUrl: params.RepositoryURL,
		HeadBranch:    params.HeadBranch,
		Since:         params.Since,
	})
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no pull request on %s", params.HeadBranch)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find the pull request on %s: %w", params.HeadBranch, err)
	}

	return s.withOneMergeReadiness(ctx, row)
}

// mergeBlockers is what about a pull request's branches turns its merge down.
type mergeBlockers struct {
	// HeadHasParent says the pull request comes from a live child, which merges only after its parent.
	HeadHasParent bool
	// BaseFinished says the pull request targets a task's branch whose own pull request merged or closed.
	BaseFinished bool
}

// GetMergeBlockers reads what turns down a merge of the pull request at an address, or ErrNotFound for one komrad has not stored.
func (s *PullRequestStore) GetMergeBlockers(ctx context.Context, url string) (*mergeBlockers, error) {
	row, err := s.queries.GetMergeBlockers(ctx, url)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no pull request at %s", url)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read what blocks a merge of the pull request at %s: %w", url, err)
	}

	return &mergeBlockers{HeadHasParent: row.HeadHasParent, BaseFinished: row.BaseFinished}, nil
}

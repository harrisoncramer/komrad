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
	"weave/lib/xslice"
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
}

// Summary is what a row about a pull request shows: how to open it and where it stands.
type Summary struct {
	Number int
	URL    string
	Title  string
	Draft  bool
	// MergedAt and ClosedAt are the forge's stamps, which decide whether it is finished.
	MergedAt *time.Time
	ClosedAt *time.Time
	// ReviewDecision is where the forge says the reviews stand, nil while it has said nothing.
	ReviewDecision *forge.ReviewDecision
	// Checks is where the forge says the checks on the head stand, nil while it reports none.
	Checks *forge.Checks
	// Mergeability is where the forge says it stands against its base and rules, nil while the
	// forge has not computed it.
	Mergeability *forge.Mergeability
}

// GetSummary is the pull request as a row shows it.
func (p PullRequest) GetSummary() Summary {
	return Summary{
		Number:         p.Number,
		URL:            p.URL,
		Title:          p.Title,
		Draft:          p.Draft,
		MergedAt:       p.MergedAt,
		ClosedAt:       p.ClosedAt,
		ReviewDecision: p.ReviewDecision,
		Checks:         p.Checks,
		Mergeability:   p.Mergeability,
	}
}

// IsFinished says whether the pull request merged or closed.
func (p PullRequest) IsFinished() bool {
	return p.MergedAt != nil || p.ClosedAt != nil
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

// Store reads and writes pull requests and their observations.
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

	return xslice.Map(rows, fromRow), nil
}

// SetStanding records where the forge said a pull request's reviews, checks and mergeability
// stand, the reviews read for the change the row holds now and the rest for the head the
// standing names, and stamps the read at readAt whether or not anything moved.
func (s *Store) SetStanding(ctx context.Context, id string, standing forge.Standing, readAt time.Time) error {
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

	err := s.queries.SetStanding(ctx, models.SetStandingParams{
		ID:             id,
		ReviewDecision: decision,
		Checks:         checks,
		Mergeability:   mergeability,
		ChecksSha:      standing.HeadSHA,
		ChecksReadAt:   readAt,
	})
	if err != nil {
		return fmt.Errorf("failed to record the standing of pull request %s: %w", id, err)
	}

	return nil
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
		ReviewDecision:    storedReviewDecision(row.ReviewDecision),
		DecisionChangedAt: row.DecisionChangedAt,
		Checks:            StoredChecks(row.Checks),
		Mergeability:      StoredMergeability(row.Mergeability),
		ChecksSHA:         xptr.Deref(row.ChecksSha),
		ChecksReadAt:      row.ChecksReadAt,
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

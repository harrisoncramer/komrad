package forge

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/lib/xhttp"
)

// ErrNotFound says the forge has nothing at the address asked for.
var ErrNotFound = fail.NotFound("the forge has nothing there")

// ErrNotRepository says a repository path is not one the forge can address.
var ErrNotRepository = fail.InvalidArgument("not a repository on this forge")

// ErrNotPullRequest says an address does not name a pull request on the forge.
var ErrNotPullRequest = fail.InvalidArgument("not a pull request on this forge")

// ErrUnauthorized says the forge refused the credential.
var ErrUnauthorized = fail.Denied("the forge refused the credential")

// ErrTooManyStandings says one Standings call asked about more pull requests than it may.
var ErrTooManyStandings = fail.InvalidArgument("too many standings for one call")

// ErrNotMergeable says the forge refused to merge a pull request.
var ErrNotMergeable = fail.Precondition("the forge will not merge the pull request")

// ErrNotDraftable says the forge refused to change whether a pull request is a draft.
var ErrNotDraftable = fail.Precondition("the forge will not change whether the pull request is a draft")

// ErrThrottled says the forge refused for its rate limit rather than for anything about the
// request.
var ErrThrottled = fail.Exhausted("the forge is rate limiting this credential")

// ThrottledError is a rate limit refusal with when the forge said to try again, which is zero
// when it said nothing.
type ThrottledError struct {
	RetryAt time.Time
}

// Error names the refusal and when to try again.
func (t *ThrottledError) Error() string {
	if t.RetryAt.IsZero() {
		return ErrThrottled.Error()
	}

	return fmt.Sprintf("%s until %s", ErrThrottled.Error(), t.RetryAt.UTC().Format(time.RFC3339))
}

// Unwrap is ErrThrottled, so errors.Is and fail.Of both see the refusal.
func (t *ThrottledError) Unwrap() error {
	return ErrThrottled
}

// RateLimit is what a forge's headers said about the credential's remaining budget on one
// answer, Known false when the headers were absent.
type RateLimit struct {
	Known     bool
	Remaining int
	// ResetAt is when the budget comes back, zero when the headers did not say.
	ResetAt time.Time
}

// IsExhausted says the budget is spent and ResetAt is when it comes back.
func (r RateLimit) IsExhausted() bool {
	return r.Known && r.Remaining == 0
}

// KindGitHub is the kind a GitHub host records.
const KindGitHub = "github"

// KindGitLab is the kind a GitLab host records.
const KindGitLab = "gitlab"

// MethodToken is the method for a credential a person supplied.
const MethodToken = "token"

// ReviewDecision is where a forge says a pull request's reviews stand, in komrad's own words.
type ReviewDecision string

const (
	// ReviewDecisionNone is a forge that has said nothing about the reviews.
	ReviewDecisionNone ReviewDecision = ""
	// ReviewDecisionPending is a review the forge requires that nobody has given.
	ReviewDecisionPending ReviewDecision = "pending"
	// ReviewDecisionApproved is the forge counting the pull request as approved.
	ReviewDecisionApproved ReviewDecision = "approved"
	// ReviewDecisionChangesRequested is a reviewer asking for changes.
	ReviewDecisionChangesRequested ReviewDecision = "changes_requested"
)

// Checks is where a forge says the checks on a pull request's head commit stand, in komrad's
// own words.
type Checks string

const (
	// ChecksNone is a commit the forge reports no checks for.
	ChecksNone Checks = ""
	// ChecksPending is a run the forge is still waiting on.
	ChecksPending Checks = "pending"
	// ChecksPassing is every check the forge counts having succeeded.
	ChecksPassing Checks = "passing"
	// ChecksFailing is at least one check the forge counts having failed.
	ChecksFailing Checks = "failing"
)

// Mergeability is where a forge says a pull request stands against its base and its branch
// rules, in komrad's own words.
type Mergeability string

const (
	// MergeabilityUnknown is a forge that has not computed the answer yet.
	MergeabilityUnknown Mergeability = ""
	// MergeabilityClean is a pull request the forge would merge now.
	MergeabilityClean Mergeability = "clean"
	// MergeabilityUnstable is a pull request whose checks are not all passing.
	MergeabilityUnstable Mergeability = "unstable"
	// MergeabilityBlocked is a branch rule the pull request does not yet satisfy.
	MergeabilityBlocked Mergeability = "blocked"
	// MergeabilityBehind is a branch the base has moved past.
	MergeabilityBehind Mergeability = "behind"
	// MergeabilityConflicting is a branch that no longer merges cleanly into its base.
	MergeabilityConflicting Mergeability = "conflicting"
)

// Standing is what a forge says about one pull request's reviews, checks and mergeability,
// and the head commit they were read for.
type Standing struct {
	Decision     ReviewDecision
	Checks       Checks
	Mergeability Mergeability
	HeadSHA      string
}

// PullRequest is one pull request as every forge describes it.
type PullRequest struct {
	// Forge is the kind of forge the pull request lives on, such as github.
	Forge string
	// ProviderID is the forge's own stable identifier, such as GitHub's node_id.
	ProviderID string
	// URL is the web address a person opens.
	URL string
	// Repository is the repository the pull request targets.
	Repository git.RemoteURL
	// Head is the repository the branch lives in, zero when the forge no longer has it.
	Head       git.RemoteURL
	Number     int
	Title      string
	HeadBranch string
	HeadSHA    string
	BaseBranch string
	BaseSHA    string
	Draft      bool
	// State is where the forge says the pull request stands, as OPEN, CLOSED or MERGED.
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
	// Decision is where the reviews stand, nil when the call did not ask.
	Decision *ReviewDecision
	// Checks is where the checks on the head commit stand, nil when the call did not ask.
	Checks *Checks
	// Mergeability is where the forge says it stands against its base and rules, nil when the
	// call did not ask.
	Mergeability *Mergeability
}

// IsFinished says whether the pull request has merged or closed.
func (pr PullRequest) IsFinished() bool {
	return pr.MergedAt != nil || pr.ClosedAt != nil
}

// Forge is one service pull requests live on, spoken to as one connection.
type Forge interface {
	Kind() string
	// Verify asks the forge who the credential belongs to.
	Verify(ctx context.Context) (login string, err error)
	// List is one page of the repository's pull requests: every one from the named branch,
	// whatever its state, or the open ones when the branch is empty.
	List(ctx context.Context, params ListParams) (*Page, error)
	// ListUpdated is one page of every pull request in the repository, whatever its state,
	// newest change first.
	ListUpdated(ctx context.Context, params ListUpdatedParams) (*Page, error)
	// ListChanged is one page of every pull request across the repositories changed at or
	// after a moment, newest first, each carrying its review decision.
	ListChanged(ctx context.Context, params ListChangedParams) (*Page, error)
	// Get is one pull request by its web address.
	Get(ctx context.Context, url string) (PullRequest, error)
	// Standings is where the forge says the reviews, checks and mergeability of up to fifty
	// pull requests stand, by number, leaving out any the forge does not have.
	Standings(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]Standing, error)
	// CommentMarks is a fingerprint of the comments on up to fifty pull requests, by number, which
	// moves when a thread or a comment is added, leaving out any the forge does not have.
	CommentMarks(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]string, error)
	// ReviewThreads is every review thread on one pull request with the comments it holds.
	ReviewThreads(ctx context.Context, repo git.RemoteURL, number int) ([]ReviewThread, error)
	// Merge lands the pull request at the web address on its base.
	Merge(ctx context.Context, url string) error
	// SetDraft marks the pull request at the web address a draft, or ready for review.
	SetDraft(ctx context.Context, url string, draft bool) error
}

// MaxStandings is the most pull requests one Standings call asks about.
const MaxStandings = 50

// ListParams says which pull requests to list and where in them to start.
type ListParams struct {
	Repo git.RemoteURL
	// Branch narrows the listing to pull requests opened from it, whatever their state; empty
	// lists the open ones.
	Branch string
	// Cursor is where a previous page ended, empty for the first, and PerPage how many a page
	// holds.
	Cursor  string
	PerPage int
}

// Page is one page of pull requests and where the next one starts.
type Page struct {
	PullRequests []PullRequest
	HasMore      bool
	// Cursor is where this page ended, to send back for the next.
	Cursor    string
	RateLimit RateLimit
}

// ListUpdatedParams says which page of a repository's pull requests by last change to read.
type ListUpdatedParams struct {
	Repo git.RemoteURL
	// Cursor is where a previous page ended, empty for the first; the page size is the most
	// the forge allows.
	Cursor string
}

// ListChangedParams says which repositories to read the changes of, and since when.
type ListChangedParams struct {
	// Repos are the repositories, every one on the forge's own host.
	Repos []git.RemoteURL
	// Since is the moment at or after which a pull request must have changed to be answered.
	Since time.Time
	// Cursor is where a previous page ended, empty for the first.
	Cursor string
}

// InstallationVerifier is a forge that can say what an installation token reaches.
type InstallationVerifier interface {
	// VerifyInstallation is how many repositories the credential's installation covers.
	VerifyInstallation(ctx context.Context) (int, error)
}

// Transport sends every request with the connection's credential and tells the
// Authenticator when the forge refuses it.
type Transport struct {
	auth Authenticator
	next http.RoundTripper
}

// NewTransport wraps a round tripper, xhttp's when nil.
func NewTransport(auth Authenticator, next http.RoundTripper) *Transport {
	if next == nil {
		next = xhttp.NewTransport()
	}

	return &Transport{auth: auth, next: next}
}

// RoundTrip sets the credential header and reports a 401 back to the Authenticator.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	credential, err := t.auth.Credential(req.Context())
	if err != nil {
		return nil, err
	}

	cloned := req.Clone(req.Context())
	cloned.Header.Set(credential.Header, credential.HeaderValue())

	resp, err := t.next.RoundTrip(cloned)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		if err := t.auth.Invalidate(req.Context()); err != nil {
			resp.Body.Close()

			return nil, fmt.Errorf("failed to record the refusal: %w", err)
		}
	}

	return resp, nil
}

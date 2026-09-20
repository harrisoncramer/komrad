package forge

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"weave/internal/fail"
	"weave/internal/git"
	"weave/lib/xhttp"
)

// ErrNotFound says the forge has nothing at the address asked for.
var ErrNotFound = fail.NotFound("the forge has nothing there")

// ErrNotRepository says a repository path is not one the forge can address.
var ErrNotRepository = fail.InvalidArgument("not a repository on this forge")

// ErrNotPullRequest says an address does not name a pull request on the forge.
var ErrNotPullRequest = fail.InvalidArgument("not a pull request on this forge")

// ErrUnauthorized says the forge refused the credential.
var ErrUnauthorized = fail.Denied("the forge refused the credential")

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
	// State is the forge's own word for where the pull request stands, kept verbatim.
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
}

// IsFinished says whether the pull request has merged or closed.
func (pr PullRequest) IsFinished() bool {
	return pr.MergedAt != nil || pr.ClosedAt != nil
}

// IsCrossRepository says whether the branch lives somewhere other than the repository it targets.
func (pr PullRequest) IsCrossRepository() bool {
	return pr.Head.IsZero() || pr.Head.Canonical() != pr.Repository.Canonical()
}

// Forge is one service pull requests live on, spoken to as one connection.
type Forge interface {
	Kind() string
	// Verify asks the forge who the credential belongs to.
	Verify(ctx context.Context) (login string, err error)
	// List is one page of the repository's pull requests: every one from the named branch,
	// whatever its state, or the open ones when the branch is empty.
	List(ctx context.Context, params ListParams) (*ListResult, error)
	// ListUpdated is one page of every pull request in the repository, whatever its state,
	// newest change first, which is what the sync pages by.
	ListUpdated(ctx context.Context, params ListUpdatedParams) (*ListUpdatedResult, error)
	// Get is one pull request by its web address.
	Get(ctx context.Context, url string) (PullRequest, error)
	// GetReviewDecisionByNumber is the forge's own word on where a pull request's reviews
	// stand, such as APPROVED or CHANGES_REQUESTED, empty when it has said nothing.
	GetReviewDecisionByNumber(ctx context.Context, repo git.RemoteURL, pullRequestNumber int) (string, error)
}

// ListParams says which pull requests to list and which page of them.
type ListParams struct {
	Repo git.RemoteURL
	// Branch narrows the listing to pull requests opened from it, whatever their state; empty
	// lists the open ones.
	Branch string
	// Page is which page to read, from 1, and PerPage how many a page holds.
	Page    int
	PerPage int
}

// ListResult is one page of pull requests and whether another follows.
type ListResult struct {
	PullRequests []PullRequest
	HasMore      bool
	RateLimit    RateLimit
}

// ListUpdatedParams says which page of a repository's pull requests to read, and the ETag the
// forge answered the same page with last time, sent so an unchanged page costs no body.
type ListUpdatedParams struct {
	Repo git.RemoteURL
	// Page is which page to read, from 1; the page size is the most the forge allows.
	Page int
	ETag string
}

// ListUpdatedResult is one page of pull requests by last change, or the forge saying the page
// has not changed since the ETag it was given.
type ListUpdatedResult struct {
	PullRequests []PullRequest
	HasMore      bool
	// ETag is what the forge answered the page with, to send back next time.
	ETag string
	// NotModified says the forge answered 304 to the ETag it was sent, and PullRequests is
	// empty.
	NotModified bool
	RateLimit   RateLimit
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

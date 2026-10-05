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

// ErrNotMergeable says the forge refused to merge a pull request.
var ErrNotMergeable = fail.Precondition("the forge will not merge the pull request")

// ErrNotDraftable says the forge refused to change whether a pull request is a draft.
var ErrNotDraftable = fail.Precondition("the forge will not change whether the pull request is a draft")

// ErrNoAutoMerge says the forge refused to turn auto-merge on or off for a pull request.
var ErrNoAutoMerge = fail.Precondition("the forge will not change auto-merge on the pull request")

// ErrNoRereview says the forge has nobody to ask for another review of a pull request.
var ErrNoRereview = fail.Precondition("the forge has nobody to ask for another review")

// ErrReplyRefused says the forge turned a reply down without posting it.
var ErrReplyRefused = fail.Precondition("the forge will not take the reply")

// ErrExpired says the forge once kept what was asked for and no longer does.
var ErrExpired = fail.NotFound("the forge no longer keeps it")

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
	// Limit is the whole budget the reset brings back, zero when the headers did not say.
	Limit int
	// ResetAt is when the budget comes back, zero when the headers did not say.
	ResetAt time.Time
}

// IsExhausted says the budget is spent and ResetAt is when it comes back.
func (r RateLimit) IsExhausted() bool {
	return r.Known && r.Remaining == 0
}

// IsLow says less than a fifth of the budget is left.
func (r RateLimit) IsLow() bool {
	return r.Known && r.Limit > 0 && r.Remaining*5 < r.Limit
}

// KindGitHub is the kind a GitHub host records.
const KindGitHub = "github"

// KindGitLab is the kind a GitLab host records.
const KindGitLab = "gitlab"

// MethodToken is the method for a credential a person supplied.
const MethodToken = "token"

// MethodRotatingToken is the method for a GitLab token komrad rotates itself before it expires.
const MethodRotatingToken = "rotating_token"

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
	// ReviewDecisionRereviewRequested is every reviewer who asked for changes asked to review again.
	ReviewDecisionRereviewRequested ReviewDecision = "rereview_requested"
)

// Checks is where the checks on a pull request's head commit stand, in komrad's own words.
type Checks string

const (
	// ChecksNone is a commit the forge reports no checks for.
	ChecksNone Checks = ""
	// ChecksPending is a run the forge is still waiting on.
	ChecksPending Checks = "pending"
	// ChecksPassing is every check the forge counts having succeeded.
	ChecksPassing Checks = "passing"
	// ChecksFailing is at least one check the forge counts having failed, with the run finished.
	ChecksFailing Checks = "failing"
	// ChecksFailingRunning is at least one check the forge counts having failed while others still
	// run.
	ChecksFailingRunning Checks = "failing_running"
)

// StatusCheckKind is what a forge reported a check as.
type StatusCheckKind string

const (
	// StatusCheckKindRun is a check run an app or workflow reported.
	StatusCheckKindRun StatusCheckKind = "check_run"
	// StatusCheckKindStatus is an old-style commit status.
	StatusCheckKindStatus StatusCheckKind = "status"
	// StatusCheckKindPipeline is a GitLab pipeline, standing as the one required check on a merge request.
	StatusCheckKindPipeline StatusCheckKind = "pipeline"
)

// StatusCheckState is where one check stands, in komrad's own words.
type StatusCheckState string

const (
	// StatusCheckStateQueued is a check waiting to start.
	StatusCheckStateQueued StatusCheckState = "queued"
	// StatusCheckStateRunning is a check that has started and not completed.
	StatusCheckStateRunning StatusCheckState = "running"
	// StatusCheckStateWaiting is a check that cannot start on its own, held for an approval or never reported.
	StatusCheckStateWaiting StatusCheckState = "waiting"
	// StatusCheckStateCompleted is a check that has finished, with a conclusion.
	StatusCheckStateCompleted StatusCheckState = "completed"
	// StatusCheckStateUnknown is a check on a commit that is no longer the head, which komrad stopped reading while it was still in flight.
	StatusCheckStateUnknown StatusCheckState = "unknown"
)

// StatusCheck is one check a forge listed on a pull request's head commit.
type StatusCheck struct {
	// ProviderID is the forge's own identifier for the check run, commit status or pipeline.
	ProviderID string
	Kind       StatusCheckKind
	// Source is the workflow that ran the check, or the app when no workflow did, empty for a status.
	Source string
	// Name is the check's name, or a commit status's context.
	Name  string
	State StatusCheckState
	// Conclusion is how a completed check ended, in the forge's own word lowercased.
	Conclusion string
	StartedAt  *time.Time
	FinishedAt *time.Time
	// Required is whether the pull request's base branch requires the check to pass.
	Required bool
	URL      string
	// Steps are the check run's steps in the order it runs them, empty for a commit status.
	Steps []StatusCheckStep
	// StepsTruncated is whether the forge listed only the first page of a longer list of steps, or could
	// not be asked for them, either of which keeps the steps stored before.
	StepsTruncated bool
	// RunID is the forge's own identifier for the run the check ran in, empty for a check that ran in none.
	RunID string
	// RunEvent is what started the run, such as pull_request or push, empty when no workflow ran the check.
	RunEvent string
	// RunStartedAt is when the run the check ran in was created.
	RunStartedAt *time.Time
	// JobID is the forge's own number for the Actions job that ran the check, which its log is read by, nil
	// for a commit status or a check run no workflow ran.
	JobID *int64
}

// StatusCheckStep is one step of a check run.
type StatusCheckStep struct {
	// Number is where the step falls in its check run, counting from one.
	Number int
	Name   string
	State  StatusCheckState
	// Conclusion is how a completed step ended, in the forge's own word lowercased.
	Conclusion string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// StatusCheckListing is the checks a forge listed on a pull request's head commit.
type StatusCheckListing struct {
	// StatusChecks is every check listed, empty when the forge answered blank.
	StatusChecks []StatusCheck
	// Truncated is whether the forge listed only the first page of a longer list of checks.
	Truncated bool
}

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

// MergeReadiness is what a forge says about one pull request's reviews, checks, mergeability and
// auto-merge.
type MergeReadiness struct {
	Decision     ReviewDecision
	Mergeability Mergeability
	AutoMerge    AutoMerge
	// Listing is every check on the head commit, nil when the forge does not list them.
	Listing *StatusCheckListing
}

// AutoMerge is whether a pull request will merge on its own once its checks and reviews pass,
// and whether the forge would let the credential ask for that now.
type AutoMerge struct {
	On      bool
	Allowed bool
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
	Author     string
	Body       string
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
	// MergeReadiness is where the reviews, checks, mergeability and auto-merge stand, nil when the call
	// did not ask.
	MergeReadiness *MergeReadiness
}

// IsFinished says whether the pull request has merged or closed.
func (pr PullRequest) IsFinished() bool {
	return pr.MergedAt != nil || pr.ClosedAt != nil
}

// IsFromItsOwnRepository says whether the branch lives in the repository the pull request targets.
func (pr PullRequest) IsFromItsOwnRepository() bool {
	return !pr.Head.IsZero() && pr.Head.Canonical() == pr.Repository.Canonical()
}

// ShouldChangeParams is what a forge read about a pull request it is about to change.
type ShouldChangeParams struct {
	Sentinel *fail.Error
	// Forge names the forge in a refusal, such as github.
	Forge string
	// Ref names the pull request in a refusal, such as #12 or !12.
	Ref    string
	Merged bool
	Closed bool
	// IsSettled says the pull request already stands the way the change asks.
	IsSettled bool
}

// ShouldChange refuses a change to a finished pull request and says whether one still open needs it.
func ShouldChange(params ShouldChangeParams) (bool, error) {
	switch {
	case params.Merged:
		return false, params.Sentinel.Saying("%s will not change %s: it has already merged", params.Forge, params.Ref)
	case params.Closed:
		return false, params.Sentinel.Saying("%s will not change %s: it was closed", params.Forge, params.Ref)
	}

	return !params.IsSettled, nil
}

// Forge is one service pull requests live on, spoken to as one connection.
type Forge interface {
	Kind() string
	// Verify asks the forge who the credential belongs to.
	Verify(ctx context.Context) (login string, err error)
	// List is one page of the repository's pull requests: every one from the named branch,
	// whatever its state, or the open ones when the branch is empty.
	List(ctx context.Context, params ListParams) (*Page, error)
	// ReadBranches is the newest pull requests opened from each branch within the repository
	// itself, each carrying its merge readiness.
	ReadBranches(ctx context.Context, repo git.RemoteURL, branches []string) (*Reading, error)
	// ReviewThreads is the review threads on one pull request with the comments they hold.
	ReviewThreads(ctx context.Context, repo git.RemoteURL, number int) (*Threads, error)
	// Merge lands the pull request at the web address on its base.
	Merge(ctx context.Context, url string) error
	// SetDraft marks the pull request at the web address a draft, or ready for review.
	SetDraft(ctx context.Context, url string, draft bool) error
	// SetAutoMerge asks the forge to merge the pull request once its checks and reviews pass, or
	// stops it from doing so.
	SetAutoMerge(ctx context.Context, params SetAutoMergeParams) error
	// RequestRereview asks every reviewer who requested changes on the pull request at the web
	// address to review it again.
	RequestRereview(ctx context.Context, url string) error
	// ReplyToThread posts a reply in a pull request's thread and answers the new comment's id.
	ReplyToThread(ctx context.Context, params ReplyToThreadParams) (string, error)
}

// ReplyToThreadParams is the thread to reply in and what to say.
type ReplyToThreadParams struct {
	Repo     git.RemoteURL
	Number   int
	ThreadID string
	OnCode   bool
	Body     string
}

// SetAutoMergeParams is the pull request to change and which way.
type SetAutoMergeParams struct {
	URL string
	// HeadSHA is the commit the person saw, which the forge refuses to queue when the branch has
	// moved past it, empty to queue whatever the head is.
	HeadSHA string
	On      bool
}

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

// Reading is what the forge said about a repository's branches, and its budget after the last
// query.
type Reading struct {
	PullRequests []PullRequest
	RateLimit    RateLimit
}

// installationVerifier is a forge that can say what an installation token reaches.
type installationVerifier interface {
	// VerifyInstallation is how many repositories the credential's installation covers.
	VerifyInstallation(ctx context.Context) (int, error)
}

// JobLogReader is a forge that can hand over the log of a finished Actions job.
type JobLogReader interface {
	// JobLog is the job's log cut at the most komrad keeps, ErrNotFound while the forge has no log ready
	// and ErrExpired once it no longer keeps one.
	JobLog(ctx context.Context, repo git.RemoteURL, jobID int64) (*JobLog, error)
}

// JobLog is the text of one job's log.
type JobLog struct {
	Text string
	// Truncated is whether the log ran past the most komrad keeps, and was cut there.
	Truncated bool
}

// TokenInfo is what a forge says of the token it was shown.
type TokenInfo struct {
	Scopes []string
	// ExpiresAt is the day the token stops working, zero for one that does not expire.
	ExpiresAt time.Time
}

// tokenRotator is a forge whose token can describe, rotate and revoke itself.
type tokenRotator interface {
	// ReadToken is what the forge says of the token it was shown.
	ReadToken(ctx context.Context) (*TokenInfo, error)
	// RotateToken trades the token it was shown for a new one that expires on the day given.
	RotateToken(ctx context.Context, expiresAt time.Time) (Secret, error)
	// RevokeToken ends the token it was shown.
	RevokeToken(ctx context.Context) error
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

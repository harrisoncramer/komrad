// Package gitlab is the forge behind gitlab.com and self-hosted GitLab, where a pull request is
// a merge request.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/lib/xhttp"
)

// DefaultAPI is where gitlab.com answers REST calls.
const DefaultAPI = "https://gitlab.com/api/v4"

// DefaultTimeout bounds one call to GitLab.
const DefaultTimeout = 30 * time.Second

// MaxPerPage is the most GitLab hands over in one page, above which it silently trims.
const MaxPerPage = 100

// stateRequestedChanges is the reviewer state GitLab records when one asks for changes.
const stateRequestedChanges = "requested_changes"

// NewParams says how the forge reaches the API and as whom.
type NewParams struct {
	Auth forge.Authenticator
	// API is the REST base, DefaultAPI when unset.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, DefaultTimeout when unset.
	Timeout time.Duration
	// Now is the clock a Retry-After is counted from, time.Now when unset.
	Now func() time.Time
}

// Forge is GitLab, spoken to over its REST API.
type Forge struct {
	api    string
	client *http.Client
	now    func() time.Time
}

// New builds the forge.
func New(params NewParams) *Forge {
	api := params.API
	if api == "" {
		api = DefaultAPI
	}

	timeout := params.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Forge{
		api:    strings.TrimRight(api, "/"),
		client: xhttp.NewClientOver(forge.NewTransport(params.Auth, params.Next), timeout),
		now:    now,
	}
}

// Kind is what the forge answers to.
func (f *Forge) Kind() string { return forge.KindGitLab }

// Verify asks GitLab who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	var user struct {
		Username string `json:"username"`
	}
	if _, err := f.get(ctx, "/user", nil, &user); err != nil {
		return "", err
	}
	if user.Username == "" {
		return "", fmt.Errorf("gitlab answered /user with no username")
	}

	return user.Username, nil
}

// List is one page of the project's merge requests, every one from the named branch or the
// open ones when no branch is named, and whether GitLab has another page.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.ListResult, error) {
	project, err := projectID(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"state":    {"opened"},
		"per_page": {strconv.Itoa(min(max(params.PerPage, 1), MaxPerPage))},
		"page":     {strconv.Itoa(max(params.Page, 1))},
	}
	if params.Branch != "" {
		query.Set("state", "all")
		query.Set("source_branch", params.Branch)
	}

	var raws []mergeRequest
	answer, err := f.get(ctx, fmt.Sprintf("/projects/%s/merge_requests?%s", project, query.Encode()), nil, &raws)
	if err != nil {
		return nil, err
	}

	return &forge.ListResult{
		PullRequests: toPullRequests(raws),
		HasMore:      hasNextPage(answer.Header),
		RateLimit:    readRateLimit(answer.Header),
	}, nil
}

// ListUpdated is one page of every merge request in the project, newest change first, or
// GitLab saying the page is unchanged since the ETag it was sent.
func (f *Forge) ListUpdated(ctx context.Context, params forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	project, err := projectID(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"state":    {"all"},
		"order_by": {"updated_at"},
		"sort":     {"desc"},
		"per_page": {strconv.Itoa(MaxPerPage)},
		"page":     {strconv.Itoa(max(params.Page, 1))},
	}
	var conditional http.Header
	if params.ETag != "" {
		conditional = http.Header{"If-None-Match": {params.ETag}}
	}

	var raws []mergeRequest
	answer, err := f.get(ctx, fmt.Sprintf("/projects/%s/merge_requests?%s", project, query.Encode()), conditional, &raws)
	if err != nil {
		return nil, err
	}

	result := &forge.ListUpdatedResult{
		ETag:      answer.Header.Get("ETag"),
		RateLimit: readRateLimit(answer.Header),
	}
	if answer.Status == http.StatusNotModified {
		result.NotModified = true

		return result, nil
	}

	result.PullRequests = toPullRequests(raws)
	result.HasMore = hasNextPage(answer.Header)

	return result, nil
}

// Get is one merge request by its web address.
func (f *Forge) Get(ctx context.Context, address string) (forge.PullRequest, error) {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return forge.PullRequest{}, err
	}

	var raw mergeRequest
	if _, err := f.get(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d", url.PathEscape(path), number), nil, &raw); err != nil {
		return forge.PullRequest{}, err
	}

	return raw.toPullRequest(), nil
}

// GetReviewDecisionByNumber reads where a merge request's reviews stand: a reviewer asking for
// changes first, then whether the approval rules are met, then whether any are still owed.
func (f *Forge) GetReviewDecisionByNumber(ctx context.Context, repo git.RemoteURL, pullRequestNumber int) (forge.ReviewDecision, error) {
	project, err := projectID(repo.Path)
	if err != nil {
		return forge.ReviewDecisionNone, err
	}

	var reviewers []struct {
		State string `json:"state"`
	}
	if _, err := f.get(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/reviewers", project, pullRequestNumber), nil, &reviewers); err != nil {
		return forge.ReviewDecisionNone, err
	}
	for _, reviewer := range reviewers {
		if reviewer.State == stateRequestedChanges {
			return forge.ReviewDecisionChangesRequested, nil
		}
	}

	var approvals struct {
		ApprovalsRequired int `json:"approvals_required"`
		ApprovalsLeft     int `json:"approvals_left"`
		ApprovedBy        []struct {
			User struct {
				ID int `json:"id"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	if _, err := f.get(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/approvals", project, pullRequestNumber), nil, &approvals); err != nil {
		return forge.ReviewDecisionNone, err
	}

	switch {
	case approvals.ApprovalsLeft > 0:
		return forge.ReviewDecisionPending, nil
	case len(approvals.ApprovedBy) > 0:
		return forge.ReviewDecisionApproved, nil
	}

	return forge.ReviewDecisionNone, nil
}

func toPullRequests(raws []mergeRequest) []forge.PullRequest {
	pullRequests := make([]forge.PullRequest, 0, len(raws))
	for _, raw := range raws {
		pullRequests = append(pullRequests, raw.toPullRequest())
	}

	return pullRequests
}

// readRateLimit is what GitLab's headers said about the credential's budget.
func readRateLimit(header http.Header) forge.RateLimit {
	remaining, err := strconv.Atoi(header.Get("RateLimit-Remaining"))
	if err != nil {
		return forge.RateLimit{}
	}

	limit := forge.RateLimit{Known: true, Remaining: remaining}
	if reset, err := strconv.ParseInt(header.Get("RateLimit-Reset"), 10, 64); err == nil {
		limit.ResetAt = time.Unix(reset, 0)
	}

	return limit
}

// throttleOf is the refusal a 429 is, with when GitLab said to try again.
func throttleOf(answer xhttp.Answer, now time.Time) *forge.ThrottledError {
	if seconds, err := strconv.Atoi(answer.Header.Get("Retry-After")); err == nil && seconds >= 0 {
		return &forge.ThrottledError{RetryAt: now.Add(time.Duration(seconds) * time.Second)}
	}

	if limit := readRateLimit(answer.Header); limit.IsExhausted() && !limit.ResetAt.IsZero() {
		return &forge.ThrottledError{RetryAt: limit.ResetAt}
	}

	return &forge.ThrottledError{}
}

// hasNextPage reads the page GitLab names after this one, empty on the last.
func hasNextPage(header http.Header) bool {
	return strings.TrimSpace(header.Get("X-Next-Page")) != ""
}

// get reads one path, with any headers the call adds, and turns GitLab's refusals into
// weave's; a 304 comes back as an answer with no body.
func (f *Forge) get(ctx context.Context, path string, extra http.Header, into any) (xhttp.Answer, error) {
	header := http.Header{}
	for key, values := range extra {
		header[key] = values
	}

	answer, err := xhttp.JSON(ctx, xhttp.JSONParams{
		Client: f.client,
		Method: http.MethodGet,
		URL:    f.api + path,
		Header: header,
	}, into)
	if err != nil {
		if failure := fail.Of(err); failure != nil {
			return xhttp.Answer{}, failure
		}

		return xhttp.Answer{}, err
	}

	switch {
	case answer.Status == http.StatusNotModified:
		return answer, nil
	case answer.Status == http.StatusUnauthorized:
		return answer, fmt.Errorf("%w: %s", forge.ErrUnauthorized, messageOf(answer.Body))
	case answer.Status == http.StatusNotFound:
		return answer, fmt.Errorf("%w: gitlab answered 404 for %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, path)
	case answer.Status == http.StatusTooManyRequests:
		return answer, throttleOf(answer, f.now())
	case !answer.IsSuccess():
		return answer, fmt.Errorf("gitlab answered %d for %s: %s", answer.Status, path, messageOf(answer.Body))
	}

	return answer, nil
}

// messageOf is what GitLab said went wrong, which its error bodies carry as message or error.
func messageOf(body []byte) string {
	var failure struct {
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(body, &failure); err == nil {
		if failure.Error != "" {
			return failure.Error
		}
		if len(failure.Message) > 0 {
			return strings.Trim(string(failure.Message), `"`)
		}
	}

	return strings.TrimSpace(string(body))
}

// projectID is the path a project is addressed by in the API, escaped as one segment, which
// is how GitLab takes a nested group path.
func projectID(path string) (string, error) {
	if path == "" || !strings.Contains(path, "/") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "", fmt.Errorf("%w: %q is not group/project", forge.ErrNotRepository, path)
	}

	return url.PathEscape(path), nil
}

// parseMergeRequestURL reads the project path and the number out of
// https://host/group/project/-/merge_requests/N.
func parseMergeRequestURL(address string) (path string, number int, err error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", 0, fmt.Errorf("%w: %q is not a gitlab merge request", forge.ErrNotPullRequest, address)
	}

	before, after, ok := strings.Cut(strings.Trim(parsed.Path, "/"), "/-/merge_requests/")
	if !ok || !strings.Contains(before, "/") {
		return "", 0, fmt.Errorf("%w: %q is not a gitlab merge request", forge.ErrNotPullRequest, address)
	}

	number, err = strconv.Atoi(strings.Split(after, "/")[0])
	if err != nil || number <= 0 {
		return "", 0, fmt.Errorf("%w: %q is not a gitlab merge request", forge.ErrNotPullRequest, address)
	}

	return before, number, nil
}

// mergeRequest is the slice of GitLab's merge request payload the record is built from.
type mergeRequest struct {
	ID              int        `json:"id"`
	IID             int        `json:"iid"`
	WebURL          string     `json:"web_url"`
	Title           string     `json:"title"`
	State           string     `json:"state"`
	Draft           bool       `json:"draft"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	MergedAt        *time.Time `json:"merged_at"`
	ClosedAt        *time.Time `json:"closed_at"`
	SourceBranch    string     `json:"source_branch"`
	TargetBranch    string     `json:"target_branch"`
	SHA             string     `json:"sha"`
	SourceProjectID int        `json:"source_project_id"`
	TargetProjectID int        `json:"target_project_id"`
	DiffRefs        *diffRefs  `json:"diff_refs"`
}

type diffRefs struct {
	BaseSHA string `json:"base_sha"`
}

func (m mergeRequest) toPullRequest() forge.PullRequest {
	repository := repositoryOf(m.WebURL)
	head := repository
	if m.SourceProjectID != m.TargetProjectID {
		head = git.RemoteURL{}
	}

	baseSHA := ""
	if m.DiffRefs != nil {
		baseSHA = m.DiffRefs.BaseSHA
	}

	return forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: strconv.Itoa(m.ID),
		URL:        m.WebURL,
		Repository: repository,
		Head:       head,
		Number:     m.IID,
		Title:      m.Title,
		HeadBranch: m.SourceBranch,
		HeadSHA:    m.SHA,
		BaseBranch: m.TargetBranch,
		BaseSHA:    baseSHA,
		Draft:      m.Draft,
		State:      m.State,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
		MergedAt:   m.MergedAt,
		ClosedAt:   closedAt(m),
	}
}

// closedAt is when the merge request finished, which GitLab stamps only on a close and weave
// records on a merge as well.
func closedAt(m mergeRequest) *time.Time {
	if m.ClosedAt != nil {
		return m.ClosedAt
	}

	return m.MergedAt
}

// repositoryOf is the project a merge request's web address sits under, zero when the address
// is not one.
func repositoryOf(address string) git.RemoteURL {
	path, _, err := parseMergeRequestURL(address)
	if err != nil {
		return git.RemoteURL{}
	}

	parsed, err := url.Parse(address)
	if err != nil {
		return git.RemoteURL{}
	}

	remote, err := git.ParseRemoteURL(parsed.Scheme + "://" + parsed.Host + "/" + path)
	if err != nil {
		return git.RemoteURL{}
	}

	return remote
}

// Build is the Builder for GitLab.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return New(NewParams{Auth: auth, API: api})
}

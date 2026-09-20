// Package github is the forge behind github.com and GitHub Enterprise.
package github

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

// DefaultAPI is where github.com answers REST calls.
const DefaultAPI = "https://api.github.com"

// DefaultTimeout bounds one call to GitHub.
const DefaultTimeout = 30 * time.Second

// MaxPerPage is the most GitHub hands over in one page, above which it silently trims.
const MaxPerPage = 100

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

// Forge is GitHub, spoken to over its REST API.
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
func (f *Forge) Kind() string { return forge.KindGitHub }

// Verify asks GitHub who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if _, err := f.get(ctx, "/user", nil, &user); err != nil {
		return "", err
	}
	if user.Login == "" {
		return "", fmt.Errorf("github answered /user with no login")
	}

	return user.Login, nil
}

// VerifyInstallation is how many repositories the installation the credential was minted for
// can reach.
func (f *Forge) VerifyInstallation(ctx context.Context) (int, error) {
	var installation struct {
		TotalCount int `json:"total_count"`
	}
	if _, err := f.get(ctx, "/installation/repositories?per_page=1", nil, &installation); err != nil {
		return 0, err
	}

	return installation.TotalCount, nil
}

// List is one page of the repository's pull requests, every one from the named branch or
// the open ones when no branch is named, and whether GitHub has another page.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.ListResult, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"state":    {"open"},
		"per_page": {strconv.Itoa(min(max(params.PerPage, 1), MaxPerPage))},
		"page":     {strconv.Itoa(max(params.Page, 1))},
	}
	if params.Branch != "" {
		query.Set("state", "all")
		query.Set("head", owner+":"+params.Branch)
	}

	var raws []pullRequest
	answer, err := f.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, name, query.Encode()), nil, &raws)
	if err != nil {
		return nil, err
	}

	return &forge.ListResult{
		PullRequests: toPullRequests(raws),
		HasMore:      hasNextPage(answer.Header),
		RateLimit:    readRateLimit(answer.Header),
	}, nil
}

// ListUpdated is one page of every pull request in the repository, newest change first, or
// GitHub saying the page is unchanged since the ETag it was sent.
func (f *Forge) ListUpdated(ctx context.Context, params forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	query := url.Values{
		"state":     {"all"},
		"sort":      {"updated"},
		"direction": {"desc"},
		"per_page":  {strconv.Itoa(MaxPerPage)},
		"page":      {strconv.Itoa(max(params.Page, 1))},
	}
	var conditional http.Header
	if params.ETag != "" {
		conditional = http.Header{"If-None-Match": {params.ETag}}
	}

	var raws []pullRequest
	answer, err := f.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, name, query.Encode()), conditional, &raws)
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

func toPullRequests(raws []pullRequest) []forge.PullRequest {
	pullRequests := make([]forge.PullRequest, 0, len(raws))
	for _, raw := range raws {
		pullRequests = append(pullRequests, raw.toPullRequest())
	}

	return pullRequests
}

// readRateLimit is what GitHub's headers said about the credential's budget.
func readRateLimit(header http.Header) forge.RateLimit {
	remaining, err := strconv.Atoi(header.Get("X-RateLimit-Remaining"))
	if err != nil {
		return forge.RateLimit{}
	}

	limit := forge.RateLimit{Known: true, Remaining: remaining}
	if reset, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		limit.ResetAt = time.Unix(reset, 0)
	}

	return limit
}

// throttleOf is the refusal a 403 or 429 is when it is GitHub's rate limit rather than a
// permission answer, nil otherwise.
func throttleOf(answer xhttp.Answer, now time.Time) *forge.ThrottledError {
	retryAfter, hasRetryAfter := readRetryAfter(answer.Header)
	limit := readRateLimit(answer.Header)
	throttled := answer.Status == http.StatusTooManyRequests ||
		hasRetryAfter ||
		limit.IsExhausted() ||
		isSecondaryLimitMessage(messageOf(answer.Body))
	if !throttled {
		return nil
	}

	switch {
	case hasRetryAfter:
		return &forge.ThrottledError{RetryAt: now.Add(retryAfter)}
	case limit.IsExhausted() && !limit.ResetAt.IsZero():
		return &forge.ThrottledError{RetryAt: limit.ResetAt}
	default:
		return &forge.ThrottledError{}
	}
}

func readRetryAfter(header http.Header) (time.Duration, bool) {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 0 {
		return 0, false
	}

	return time.Duration(seconds) * time.Second, true
}

// isSecondaryLimitMessage recognises the body GitHub sends for its secondary limit, which can
// come with neither rate limit header.
func isSecondaryLimitMessage(message string) bool {
	lowered := strings.ToLower(message)

	return strings.Contains(lowered, "secondary rate limit") || strings.Contains(lowered, "abuse detection")
}

// hasNextPage reads GitHub's Link header for a page after this one.
func hasNextPage(header http.Header) bool {
	for _, link := range header.Values("Link") {
		if strings.Contains(link, `rel="next"`) {
			return true
		}
	}

	return false
}

// Get is one pull request by its web address.
func (f *Forge) Get(ctx context.Context, address string) (forge.PullRequest, error) {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return forge.PullRequest{}, err
	}

	var raw pullRequest
	if _, err := f.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, name, number), nil, &raw); err != nil {
		return forge.PullRequest{}, err
	}

	return raw.toPullRequest(), nil
}

// get reads one path, with any headers the call adds to the ones every GitHub call carries,
// and turns GitHub's refusals into weave's; a 304 comes back as an answer with no body.
func (f *Forge) get(ctx context.Context, path string, extra http.Header, into any) (xhttp.Answer, error) {
	header := http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-GitHub-Api-Version": {"2022-11-28"},
	}
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
		return answer, fmt.Errorf("%w: github answered 404 for %s, which is also its answer for a repository the credential cannot see", forge.ErrNotFound, path)
	case answer.Status == http.StatusForbidden || answer.Status == http.StatusTooManyRequests:
		if throttled := throttleOf(answer, f.now()); throttled != nil {
			return answer, throttled
		}

		return answer, fmt.Errorf("github answered %d for %s: %s", answer.Status, path, messageOf(answer.Body))
	case !answer.IsSuccess():
		return answer, fmt.Errorf("github answered %d for %s: %s", answer.Status, path, messageOf(answer.Body))
	}

	return answer, nil
}

// messageOf is what GitHub said went wrong, which its error bodies carry as message.
func messageOf(body []byte) string {
	var failure struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &failure); err == nil && failure.Message != "" {
		return failure.Message
	}

	return strings.TrimSpace(string(body))
}

// ownerAndName splits a repository path, which on GitHub is always two segments.
func ownerAndName(path string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("%w: %q is not owner/name", forge.ErrNotRepository, path)
	}

	return owner, name, nil
}

// parsePullURL reads the owner, name and number out of https://host/owner/name/pull/N.
func parsePullURL(address string) (owner, name string, number int, err error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 4 || segments[2] != "pull" {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	number, err = strconv.Atoi(segments[3])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	return segments[0], segments[1], number, nil
}

// pullRequest is the slice of GitHub's pull request payload the record is built from.
type pullRequest struct {
	NodeID    string     `json:"node_id"`
	HTMLURL   string     `json:"html_url"`
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Draft     bool       `json:"draft"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	MergedAt  *time.Time `json:"merged_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Head      ref        `json:"head"`
	Base      ref        `json:"base"`
}

type ref struct {
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Repo *repo  `json:"repo"`
}

type repo struct {
	HTMLURL string `json:"html_url"`
}

func (p pullRequest) toPullRequest() forge.PullRequest {
	return forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: p.NodeID,
		URL:        p.HTMLURL,
		Repository: remoteOf(p.Base.Repo),
		Head:       remoteOf(p.Head.Repo),
		Number:     p.Number,
		Title:      p.Title,
		HeadBranch: p.Head.Ref,
		HeadSHA:    p.Head.SHA,
		BaseBranch: p.Base.Ref,
		BaseSHA:    p.Base.SHA,
		Draft:      p.Draft,
		State:      p.State,
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
		MergedAt:   p.MergedAt,
		ClosedAt:   p.ClosedAt,
	}
}

// remoteOf is the repository a payload names, zero for one GitHub no longer has.
func remoteOf(r *repo) git.RemoteURL {
	if r == nil {
		return git.RemoteURL{}
	}

	remote, err := git.ParseRemoteURL(r.HTMLURL)
	if err != nil {
		return git.RemoteURL{}
	}

	return remote
}

// Build is the Builder for GitHub.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return New(NewParams{Auth: auth, API: api})
}

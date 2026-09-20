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
	"weave/lib/xptr"
)

// DefaultAPI is where github.com answers REST calls, with GraphQL beside it.
const DefaultAPI = "https://api.github.com"

// DefaultTimeout bounds one call to GitHub.
const DefaultTimeout = 30 * time.Second

// MaxPerPage is the most GitHub hands over in one page, above which it silently trims.
const MaxPerPage = 100

// pullRequestFragment is every field of a pull request the record is built from.
const pullRequestFragment = `fragment pullRequestFields on PullRequest {
  id number title url state isDraft createdAt updatedAt mergedAt closedAt
  headRefName headRefOid baseRefName baseRefOid headRepository { url } baseRepository { url }
}`

const verifyQuery = `query Verify { viewer { login } }`

const getQuery = `query Get($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { ...pullRequestFields } }
}
` + pullRequestFragment

const listQuery = `query List($owner: String!, $name: String!, $first: Int!, $after: String, $branch: String, $states: [PullRequestState!]) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: $first, after: $after, headRefName: $branch, states: $states, orderBy: {field: CREATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes { ...pullRequestFields }
    }
  }
}
` + pullRequestFragment

const listUpdatedQuery = `query ListUpdated($owner: String!, $name: String!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    pullRequests(first: $first, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes { ...pullRequestFields }
    }
  }
}
` + pullRequestFragment

const listChangedQuery = `query ListChanged($query: String!, $first: Int!, $after: String) {
  search(type: ISSUE, query: $query, first: $first, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest { ...pullRequestFields reviewDecision } }
  }
}
` + pullRequestFragment

// NewParams says how the forge reaches the API and as whom.
type NewParams struct {
	Auth forge.Authenticator
	// API is the REST base, DefaultAPI when unset, beside which GraphQL is answered.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, DefaultTimeout when unset.
	Timeout time.Duration
	// Now is the clock a Retry-After is counted from, time.Now when unset.
	Now func() time.Time
}

// Forge is GitHub, spoken to over its GraphQL API.
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
	var data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	answer, err := f.query(ctx, verifyQuery, nil, &data)
	if err != nil {
		return "", err
	}
	if err := answer.failure(); err != nil {
		return "", err
	}
	if data.Viewer.Login == "" {
		return "", fmt.Errorf("github answered viewer with no login")
	}

	return data.Viewer.Login, nil
}

// VerifyInstallation is how many repositories the installation the credential was minted for
// can reach, over REST, which is the one call GitHub has no GraphQL field for.
func (f *Forge) VerifyInstallation(ctx context.Context) (int, error) {
	var installation struct {
		TotalCount int `json:"total_count"`
	}
	answer, err := f.send(ctx, xhttp.JSONParams{
		Method: http.MethodGet,
		URL:    f.api + "/installation/repositories?per_page=1",
	}, &installation)
	if err != nil {
		return 0, err
	}
	if answer.Status == http.StatusNotFound {
		return 0, fmt.Errorf("%w: github answered 404 for /installation/repositories, which is also its answer for a repository the credential cannot see", forge.ErrNotFound)
	}

	return installation.TotalCount, nil
}

// List is one page of the repository's pull requests, every one from the named branch or
// the open ones when no branch is named, and where the next page starts.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.ListResult, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{
		"owner":  owner,
		"name":   name,
		"first":  min(max(params.PerPage, 1), MaxPerPage),
		"after":  cursorOf(params.Cursor),
		"states": []string{"OPEN"},
	}
	if params.Branch != "" {
		variables["branch"] = params.Branch
		variables["states"] = []string{"OPEN", "CLOSED", "MERGED"}
	}

	page, limit, err := f.listPullRequests(ctx, listQuery, variables, owner, name)
	if err != nil {
		return nil, err
	}

	return &forge.ListResult{
		PullRequests: page.toPullRequests(false),
		HasMore:      page.PageInfo.HasNextPage,
		Cursor:       page.PageInfo.EndCursor,
		RateLimit:    limit,
	}, nil
}

// ListUpdated is one page of every pull request in the repository, newest change first, and
// where the next page starts.
func (f *Forge) ListUpdated(ctx context.Context, params forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	page, limit, err := f.listPullRequests(ctx, listUpdatedQuery, map[string]any{
		"owner": owner,
		"name":  name,
		"first": MaxPerPage,
		"after": cursorOf(params.Cursor),
	}, owner, name)
	if err != nil {
		return nil, err
	}

	return &forge.ListUpdatedResult{
		PullRequests: page.toPullRequests(false),
		HasMore:      page.PageInfo.HasNextPage,
		Cursor:       page.PageInfo.EndCursor,
		RateLimit:    limit,
	}, nil
}

// ListChanged is one page of every pull request across the repositories changed at or after
// Since, newest first, each with its review decision, read through GitHub's search.
func (f *Forge) ListChanged(ctx context.Context, params forge.ListChangedParams) (*forge.ListChangedResult, error) {
	terms := []string{"is:pr"}
	for _, repo := range params.Repos {
		owner, name, err := ownerAndName(repo.Path)
		if err != nil {
			return nil, err
		}

		terms = append(terms, "repo:"+owner+"/"+name)
	}
	terms = append(terms, "updated:>="+params.Since.UTC().Format(time.RFC3339), "sort:updated-desc")

	var data struct {
		Search connection `json:"search"`
	}
	answer, err := f.query(ctx, listChangedQuery, map[string]any{
		"query": strings.Join(terms, " "),
		"first": MaxPerPage,
		"after": cursorOf(params.Cursor),
	}, &data)
	if err != nil {
		return nil, err
	}
	if err := answer.failure(); err != nil {
		return nil, err
	}

	return &forge.ListChangedResult{
		PullRequests: data.Search.toPullRequests(true),
		HasMore:      data.Search.PageInfo.HasNextPage,
		Cursor:       data.Search.PageInfo.EndCursor,
		RateLimit:    answer.rateLimit,
	}, nil
}

// Get is one pull request by its web address.
func (f *Forge) Get(ctx context.Context, address string) (forge.PullRequest, error) {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return forge.PullRequest{}, err
	}

	var data struct {
		Repository *struct {
			PullRequest *pullRequestNode `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.query(ctx, getQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if err := answer.failure(); err != nil {
		return forge.PullRequest{}, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return forge.PullRequest{}, fmt.Errorf("%w: github has no pull request %d in %s/%s", forge.ErrNotFound, number, owner, name)
	}

	return data.Repository.PullRequest.toPullRequest(false), nil
}

// ReviewDecisions reads GitHub's computed reviewDecision for up to fifty pull requests in one
// query, by alias, leaving out any GitHub does not have.
func (f *Forge) ReviewDecisions(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]forge.ReviewDecision, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}
	if len(numbers) > forge.MaxReviewDecisions {
		return nil, fail.InvalidArgument("too many review decisions for one call").Saying("at most %d review decisions can be read in one call, not %d", forge.MaxReviewDecisions, len(numbers))
	}

	var fields strings.Builder
	for _, number := range numbers {
		fmt.Fprintf(&fields, "\n    p%d: pullRequest(number: %d) { reviewDecision }", number, number)
	}
	operation := "query ReviewDecisions($owner: String!, $name: String!) {\n  repository(owner: $owner, name: $name) {" + fields.String() + "\n  }\n}"

	var data struct {
		Repository map[string]*struct {
			ReviewDecision *string `json:"reviewDecision"`
		} `json:"repository"`
	}
	answer, err := f.query(ctx, operation, map[string]any{"owner": owner, "name": name}, &data)
	if err != nil {
		return nil, err
	}
	if err := answer.failureOutside("repository"); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, fmt.Errorf("%w: github has no repository %s/%s, which is also its answer for one the credential cannot see", forge.ErrNotFound, owner, name)
	}

	decisions := make(map[int]forge.ReviewDecision, len(numbers))
	for _, number := range numbers {
		node := data.Repository["p"+strconv.Itoa(number)]
		if node == nil {
			continue
		}

		decisions[number] = reviewDecisionOf(node.ReviewDecision)
	}

	return decisions, nil
}

// listPullRequests reads one page of a repository's pullRequests connection, whichever query
// shapes it.
func (f *Forge) listPullRequests(ctx context.Context, operation string, variables map[string]any, owner, name string) (*connection, forge.RateLimit, error) {
	var data struct {
		Repository *struct {
			PullRequests connection `json:"pullRequests"`
		} `json:"repository"`
	}
	answer, err := f.query(ctx, operation, variables, &data)
	if err != nil {
		return nil, forge.RateLimit{}, err
	}
	if err := answer.failure(); err != nil {
		return nil, forge.RateLimit{}, err
	}
	if data.Repository == nil {
		return nil, forge.RateLimit{}, fmt.Errorf("%w: github has no repository %s/%s, which is also its answer for one the credential cannot see", forge.ErrNotFound, owner, name)
	}

	return &data.Repository.PullRequests, answer.rateLimit, nil
}

// cursorOf is a cursor as the query variable takes it, null for the first page.
func cursorOf(cursor string) *string {
	if cursor == "" {
		return nil
	}

	return xptr.Ptr(cursor)
}

// reviewDecisionOf is GitHub's reviewDecision in weave's words, none for a null.
func reviewDecisionOf(decision *string) forge.ReviewDecision {
	if decision == nil {
		return forge.ReviewDecisionNone
	}

	switch *decision {
	case "APPROVED":
		return forge.ReviewDecisionApproved
	case "CHANGES_REQUESTED":
		return forge.ReviewDecisionChangesRequested
	case "REVIEW_REQUIRED":
		return forge.ReviewDecisionPending
	}

	return forge.ReviewDecisionNone
}

// graphqlURL is where the host answers GraphQL, beside the REST base rather than under it.
func (f *Forge) graphqlURL() string {
	return strings.TrimSuffix(f.api, "/v3") + "/graphql"
}

// graphqlError is one entry of the errors a GraphQL answer carries beside its data.
type graphqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    []any  `json:"path"`
}

// graphqlAnswer is what one query came back with besides its data.
type graphqlAnswer struct {
	errors    []graphqlError
	rateLimit forge.RateLimit
}

// failure is the first error the answer carries as weave's, nil when it carries none.
func (a *graphqlAnswer) failure() error {
	return a.failureOutside("")
}

// failureOutside is failure with a NOT_FOUND under the named field forgiven, which is how a
// query by alias reports one alias GitHub has nothing for.
func (a *graphqlAnswer) failureOutside(field string) error {
	for _, failure := range a.errors {
		if failure.Type == "NOT_FOUND" {
			if field != "" && len(failure.Path) == 2 && failure.Path[0] == field {
				continue
			}

			return fmt.Errorf("%w: %s, which is also github's answer for a repository the credential cannot see", forge.ErrNotFound, failure.Message)
		}

		return fmt.Errorf("github refused the query: %s", failure.Message)
	}

	return nil
}

// query sends one named operation to GitHub's GraphQL endpoint, decodes its data, and turns
// GitHub's refusals into weave's, leaving the errors beside the data for the caller to read.
func (f *Forge) query(ctx context.Context, operation string, variables map[string]any, into any) (*graphqlAnswer, error) {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphqlError  `json:"errors"`
	}
	answer, err := f.send(ctx, xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    f.graphqlURL(),
		Body:   map[string]any{"query": operation, "variables": variables},
	}, &envelope)
	if err != nil {
		return nil, err
	}
	if answer.Status == http.StatusNotFound {
		return nil, fmt.Errorf("github answered 404 at %s, which is not where its GraphQL lives", f.graphqlURL())
	}

	limit := readRateLimit(answer.Header)
	for _, failure := range envelope.Errors {
		if failure.Type == "RATE_LIMITED" {
			return nil, &forge.ThrottledError{RetryAt: limit.ResetAt}
		}
	}

	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, into); err != nil {
			return nil, fmt.Errorf("failed to decode github's answer to %s: %w", operationOf(operation), err)
		}
	}

	return &graphqlAnswer{errors: envelope.Errors, rateLimit: limit}, nil
}

// operationOf is the name a query was written with.
func operationOf(operation string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(operation, "query "), "(")
	name, _, _ = strings.Cut(name, " ")

	return name
}

// send makes one call with the headers every GitHub call carries and turns GitHub's refusals
// into weave's, leaving a 404 to the caller since the two APIs mean different things by it.
func (f *Forge) send(ctx context.Context, params xhttp.JSONParams, into any) (xhttp.Answer, error) {
	if params.Header == nil {
		params.Header = http.Header{}
	}
	params.Header.Set("Accept", "application/vnd.github+json")
	params.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	params.Client = f.client
	path := strings.TrimPrefix(params.URL, f.api)

	answer, err := xhttp.JSON(ctx, params, into)
	if err != nil {
		if failure := fail.Of(err); failure != nil {
			return xhttp.Answer{}, failure
		}

		return xhttp.Answer{}, err
	}

	switch {
	case answer.Status == http.StatusUnauthorized:
		return answer, fmt.Errorf("%w: %s", forge.ErrUnauthorized, messageOf(answer.Body))
	case answer.Status == http.StatusNotFound:
		return answer, nil
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

// connection is one page of a pull request connection as GitHub answers it.
type connection struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []pullRequestNode `json:"nodes"`
}

func (c *connection) toPullRequests(withDecision bool) []forge.PullRequest {
	pullRequests := make([]forge.PullRequest, 0, len(c.Nodes))
	for _, node := range c.Nodes {
		pullRequests = append(pullRequests, node.toPullRequest(withDecision))
	}

	return pullRequests
}

// pullRequestNode is the slice of GitHub's pull request node the record is built from.
type pullRequestNode struct {
	ID             string          `json:"id"`
	Number         int             `json:"number"`
	Title          string          `json:"title"`
	URL            string          `json:"url"`
	State          string          `json:"state"`
	IsDraft        bool            `json:"isDraft"`
	ReviewDecision *string         `json:"reviewDecision"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	MergedAt       *time.Time      `json:"mergedAt"`
	ClosedAt       *time.Time      `json:"closedAt"`
	HeadRefName    string          `json:"headRefName"`
	HeadRefOid     string          `json:"headRefOid"`
	BaseRefName    string          `json:"baseRefName"`
	BaseRefOid     string          `json:"baseRefOid"`
	HeadRepository *repositoryNode `json:"headRepository"`
	BaseRepository *repositoryNode `json:"baseRepository"`
}

type repositoryNode struct {
	URL string `json:"url"`
}

// toPullRequest is the node as the record, carrying its decision when the query asked for one.
func (n pullRequestNode) toPullRequest(withDecision bool) forge.PullRequest {
	pullRequest := forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: n.ID,
		URL:        n.URL,
		Repository: remoteOf(n.BaseRepository),
		Head:       remoteOf(n.HeadRepository),
		Number:     n.Number,
		Title:      n.Title,
		HeadBranch: n.HeadRefName,
		HeadSHA:    n.HeadRefOid,
		BaseBranch: n.BaseRefName,
		BaseSHA:    n.BaseRefOid,
		Draft:      n.IsDraft,
		State:      n.State,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		MergedAt:   n.MergedAt,
		ClosedAt:   n.ClosedAt,
	}
	if withDecision {
		pullRequest.Decision = xptr.Ptr(reviewDecisionOf(n.ReviewDecision))
	}

	return pullRequest
}

// remoteOf is the repository a node names, zero for one GitHub no longer has.
func remoteOf(node *repositoryNode) git.RemoteURL {
	if node == nil {
		return git.RemoteURL{}
	}

	remote, err := git.ParseRemoteURL(node.URL)
	if err != nil {
		return git.RemoteURL{}
	}

	return remote
}

// Build is the Builder for GitHub.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return New(NewParams{Auth: auth, API: api})
}

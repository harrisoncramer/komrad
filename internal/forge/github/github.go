// Package github is the forge behind github.com and GitHub Enterprise.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
	"komrad/lib/xhttp"
	"komrad/lib/xptr"
)

// DefaultAPI is where github.com answers REST calls, with GraphQL beside it.
const DefaultAPI = "https://api.github.com"

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

const mergeTargetQuery = `query MergeTarget($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { viewerDefaultMergeMethod pullRequest(number: $number) { id } }
}`

const mergeMutation = `mutation Merge($id: ID!, $method: PullRequestMergeMethod!) {
  mergePullRequest(input: {pullRequestId: $id, mergeMethod: $method}) { clientMutationId }
}`

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
    nodes { ... on PullRequest { ...pullRequestFields ` + standingFields + ` } }
  }
}
` + pullRequestFragment

// standingFields is what the reviews, checks and mergeability of a pull request are read
// from: GitHub's computed review decision, the rollup of every check on the head commit and
// its computed merge state.
const standingFields = `reviewDecision mergeStateStatus commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }`

// dialect is what GitHub does differently on the wire.
var dialect = graphql.Dialect{
	Name: "github",
	Header: http.Header{
		"Accept":               {"application/vnd.github+json"},
		"X-Github-Api-Version": {"2022-11-28"},
	},
	RateLimitHeader: "X-RateLimit",
	Version:         "/v3",
}

// NewForgeParams says how the forge reaches the API and as whom.
type NewForgeParams struct {
	Auth forge.Authenticator
	// API is the REST base, DefaultAPI when unset, beside which GraphQL is answered.
	API string
	// Next is the round tripper under the credential transport, the default when nil.
	Next http.RoundTripper
	// Timeout bounds one call, graphql.DefaultTimeout when unset.
	Timeout time.Duration
	// Now is the clock a Retry-After is counted from, time.Now when unset.
	Now func() time.Time
}

// Forge is GitHub, spoken to over its GraphQL API.
type Forge struct {
	client *graphql.Client
}

// NewForge builds the forge.
func NewForge(params NewForgeParams) *Forge {
	api := params.API
	if api == "" {
		api = DefaultAPI
	}

	return &Forge{client: graphql.NewClient(graphql.NewClientParams{
		Dialect: dialect,
		Auth:    params.Auth,
		API:     api,
		Next:    params.Next,
		Timeout: params.Timeout,
		Now:     params.Now,
	})}
}

// Kind is what the forge answers to.
func (f *Forge) Kind() string {
	return forge.KindGitHub
}

// Verify asks GitHub who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	var data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	answer, err := f.client.Query(ctx, verifyQuery, nil, &data)
	if err != nil {
		return "", err
	}
	if err := failureOf(answer, ""); err != nil {
		return "", err
	}
	if data.Viewer.Login == "" {
		return "", fmt.Errorf("github answered viewer with no login")
	}

	return data.Viewer.Login, nil
}

// VerifyInstallation is how many repositories the installation the credential was minted for
// can reach, over REST, the one call GitHub has no GraphQL field for.
func (f *Forge) VerifyInstallation(ctx context.Context) (int, error) {
	var installation struct {
		TotalCount int `json:"total_count"`
	}
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodGet,
		URL:    f.client.API() + "/installation/repositories?per_page=1",
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
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.Page, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{
		"owner":  owner,
		"name":   name,
		"first":  min(max(params.PerPage, 1), MaxPerPage),
		"after":  graphql.CursorOf(params.Cursor),
		"states": []string{"OPEN"},
	}
	if params.Branch != "" {
		variables["branch"] = params.Branch
		variables["states"] = []string{"OPEN", "CLOSED", "MERGED"}
	}

	return f.listPullRequests(ctx, listQuery, variables)
}

// ListUpdated is one page of every pull request in the repository, newest change first, and
// where the next page starts.
func (f *Forge) ListUpdated(ctx context.Context, params forge.ListUpdatedParams) (*forge.Page, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	return f.listPullRequests(ctx, listUpdatedQuery, map[string]any{
		"owner": owner,
		"name":  name,
		"first": MaxPerPage,
		"after": graphql.CursorOf(params.Cursor),
	})
}

// ListChanged is one page of every pull request across the repositories changed at or after
// Since, newest first, each with its review decision, read through GitHub's search.
func (f *Forge) ListChanged(ctx context.Context, params forge.ListChangedParams) (*forge.Page, error) {
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
	answer, err := f.client.Query(ctx, listChangedQuery, map[string]any{
		"query": strings.Join(terms, " "),
		"first": MaxPerPage,
		"after": graphql.CursorOf(params.Cursor),
	}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer, ""); err != nil {
		return nil, err
	}

	return graphql.PageOf(&data.Search, answer.RateLimit, func(node pullRequestNode) forge.PullRequest {
		return node.toPullRequest(true)
	}), nil
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
	answer, err := f.client.Query(ctx, getQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if err := failureOf(answer, ""); err != nil {
		return forge.PullRequest{}, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return forge.PullRequest{}, fmt.Errorf("%w: github has no pull request %d in %s/%s", forge.ErrNotFound, number, owner, name)
	}

	return data.Repository.PullRequest.toPullRequest(false), nil
}

// Standings reads GitHub's computed reviewDecision and the check rollup of the head commit for
// up to fifty pull requests in one query, by alias, leaving out any GitHub does not have.
func (f *Forge) Standings(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]forge.Standing, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}
	if len(numbers) > forge.MaxStandings {
		return nil, forge.ErrTooManyStandings.Saying("at most %d standings can be read in one call, not %d", forge.MaxStandings, len(numbers))
	}

	var fields strings.Builder
	for _, number := range numbers {
		fmt.Fprintf(&fields, "\n    p%d: pullRequest(number: %d) { headRefOid %s }", number, number, standingFields)
	}
	operation := "query Standings($owner: String!, $name: String!) {\n  repository(owner: $owner, name: $name) {" + fields.String() + "\n  }\n}"

	var data struct {
		Repository map[string]*standingNode `json:"repository"`
	}
	variables := map[string]any{"owner": owner, "name": name}
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer, "repository"); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}

	standings := make(map[int]forge.Standing, len(numbers))
	for _, number := range numbers {
		node := data.Repository["p"+strconv.Itoa(number)]
		if node == nil {
			continue
		}

		standings[number] = node.standing()
	}

	return standings, nil
}

// Merge lands the pull request with the method GitHub offers the credential by default.
func (f *Forge) Merge(ctx context.Context, address string) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			ViewerDefaultMergeMethod string `json:"viewerDefaultMergeMethod"`
			PullRequest              *struct {
				ID string `json:"id"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, mergeTargetQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return err
	}
	if err := failureOf(answer, ""); err != nil {
		return err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return fmt.Errorf("%w: github has no pull request %d in %s/%s", forge.ErrNotFound, number, owner, name)
	}

	var merged struct{}
	answer, err = f.client.Query(ctx, mergeMutation, map[string]any{
		"id":     data.Repository.PullRequest.ID,
		"method": data.Repository.ViewerDefaultMergeMethod,
	}, &merged)
	if err != nil {
		return err
	}
	if len(answer.Errors) > 0 {
		return forge.ErrNotMergeable.Saying("github refused to merge %s: %s", address, answer.Errors[0].Message)
	}

	return nil
}

// listPullRequests reads one page of a repository's pullRequests connection, whichever query
// shapes it.
func (f *Forge) listPullRequests(ctx context.Context, operation string, variables map[string]any) (*forge.Page, error) {
	var data struct {
		Repository *struct {
			PullRequests connection `json:"pullRequests"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer, ""); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}

	return graphql.PageOf(&data.Repository.PullRequests, answer.RateLimit, func(node pullRequestNode) forge.PullRequest {
		return node.toPullRequest(false)
	}), nil
}

// noRepository is GitHub answering null for the repository a query's variables name.
func noRepository(variables map[string]any) error {
	return fmt.Errorf("%w: github has no repository %s/%s, which is also its answer for one the credential cannot see", forge.ErrNotFound, variables["owner"], variables["name"])
}

// failureOf is the first error the answer carries as komrad's, nil when it carries none, with a
// NOT_FOUND under one alias of the named field forgiven.
func failureOf(answer *graphql.Answer, field string) error {
	for _, failure := range answer.Errors {
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

// checksOf is GitHub's statusCheckRollup state in komrad's words, none for a commit with no
// rollup.
func checksOf(rollup *rollupNode) forge.Checks {
	if rollup == nil {
		return forge.ChecksNone
	}

	switch rollup.State {
	case "SUCCESS":
		return forge.ChecksPassing
	case "FAILURE", "ERROR":
		return forge.ChecksFailing
	case "PENDING", "EXPECTED":
		return forge.ChecksPending
	}

	return forge.ChecksNone
}

// reviewDecisionOf is GitHub's reviewDecision in komrad's words, none for a null.
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
type connection = graphql.Connection[pullRequestNode]

// pullRequestNode is the slice of GitHub's pull request node the record is built from.
type pullRequestNode struct {
	standingNode
	ID             string          `json:"id"`
	Number         int             `json:"number"`
	Title          string          `json:"title"`
	URL            string          `json:"url"`
	State          string          `json:"state"`
	IsDraft        bool            `json:"isDraft"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	MergedAt       *time.Time      `json:"mergedAt"`
	ClosedAt       *time.Time      `json:"closedAt"`
	HeadRefName    string          `json:"headRefName"`
	BaseRefName    string          `json:"baseRefName"`
	BaseRefOid     string          `json:"baseRefOid"`
	HeadRepository *repositoryNode `json:"headRepository"`
	BaseRepository *repositoryNode `json:"baseRepository"`
}

type repositoryNode struct {
	URL string `json:"url"`
}

// standingNode is what GitHub says about a pull request's reviews, the checks on its head and
// its merge state.
type standingNode struct {
	HeadRefOid       string  `json:"headRefOid"`
	ReviewDecision   *string `json:"reviewDecision"`
	MergeStateStatus string  `json:"mergeStateStatus"`
	Commits          struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *rollupNode `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type rollupNode struct {
	State string `json:"state"`
}

// standing is the node's decision, checks and mergeability with the head they were read for.
func (n standingNode) standing() forge.Standing {
	return forge.Standing{
		Decision:     reviewDecisionOf(n.ReviewDecision),
		Checks:       n.checks(),
		Mergeability: mergeabilityOf(n.MergeStateStatus),
		HeadSHA:      n.HeadRefOid,
	}
}

// mergeabilityOf is GitHub's mergeStateStatus in komrad's words.
func mergeabilityOf(status string) forge.Mergeability {
	switch status {
	case "CLEAN", "HAS_HOOKS":
		return forge.MergeabilityClean
	case "UNSTABLE":
		return forge.MergeabilityUnstable
	case "BLOCKED", "DRAFT":
		return forge.MergeabilityBlocked
	case "BEHIND":
		return forge.MergeabilityBehind
	case "DIRTY":
		return forge.MergeabilityConflicting
	}

	return forge.MergeabilityUnknown
}

// checks is the rollup of the head commit in komrad's words, none when the query carried no
// commit.
func (n standingNode) checks() forge.Checks {
	if len(n.Commits.Nodes) == 0 {
		return forge.ChecksNone
	}

	return checksOf(n.Commits.Nodes[len(n.Commits.Nodes)-1].Commit.StatusCheckRollup)
}

// remote is the repository the node names, zero for one GitHub no longer has.
func (n *repositoryNode) remote() git.RemoteURL {
	if n == nil {
		return git.RemoteURL{}
	}

	return graphql.RemoteOf(n.URL)
}

// toPullRequest is the node as the record, carrying its decision and checks when the query
// asked for them.
func (n pullRequestNode) toPullRequest(withStanding bool) forge.PullRequest {
	pullRequest := forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: n.ID,
		URL:        n.URL,
		Repository: n.BaseRepository.remote(),
		Head:       n.HeadRepository.remote(),
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
	if withStanding {
		pullRequest.Decision = xptr.Ptr(reviewDecisionOf(n.ReviewDecision))
		pullRequest.Checks = xptr.Ptr(n.checks())
		pullRequest.Mergeability = xptr.Ptr(mergeabilityOf(n.MergeStateStatus))
	}

	return pullRequest
}

// Build is the Builder for GitHub.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return NewForge(NewForgeParams{Auth: auth, API: api})
}

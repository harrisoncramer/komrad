// Package github is the forge behind github.com and GitHub Enterprise.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
	"komrad/lib/xhttp"
	"komrad/lib/xptr"
)

// defaultAPI is where github.com answers REST calls, with GraphQL beside it.
const defaultAPI = "https://api.github.com"

// maxPerPage is the most GitHub hands over in one page, above which it silently trims.
const maxPerPage = 100

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
  repository(owner: $owner, name: $name) { viewerDefaultMergeMethod pullRequest(number: $number) { id state } }
}`

const mergeMutation = `mutation Merge($id: ID!, $method: PullRequestMergeMethod!) {
  mergePullRequest(input: {pullRequestId: $id, mergeMethod: $method}) { clientMutationId }
}`

const draftTargetQuery = `query DraftTarget($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) { id state isDraft } }
}`

const convertToDraftMutation = `mutation ConvertToDraft($id: ID!) {
  convertPullRequestToDraft(input: {pullRequestId: $id}) { clientMutationId }
}`

const markReadyMutation = `mutation MarkReady($id: ID!) {
  markPullRequestReadyForReview(input: {pullRequestId: $id}) { clientMutationId }
}`

const autoMergeTargetQuery = `query AutoMergeTarget($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) { viewerDefaultMergeMethod pullRequest(number: $number) { id state autoMergeRequest { enabledAt } } }
}`

const enableAutoMergeMutation = `mutation EnableAutoMerge($id: ID!, $method: PullRequestMergeMethod!, $head: GitObjectID) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method, expectedHeadOid: $head}) { clientMutationId }
}`

const disableAutoMergeMutation = `mutation DisableAutoMerge($id: ID!) {
  disablePullRequestAutoMerge(input: {pullRequestId: $id}) { clientMutationId }
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

// standingFields is what the reviews, checks, mergeability and auto-merge of a pull request
// are read from: GitHub's computed review decision, the rollup of every check on the head
// commit, its computed merge state, the auto-merge request on it and whether the credential
// may make one.
const standingFields = `reviewDecision mergeStateStatus commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } autoMergeRequest { enabledAt } viewerCanEnableAutoMerge`

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
	// API is the REST base, https://api.github.com when unset, beside which GraphQL is answered.
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
		api = defaultAPI
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
	if err := failureOf(answer); err != nil {
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
		"first":  min(max(params.PerPage, 1), maxPerPage),
		"after":  graphql.CursorOf(params.Cursor),
		"states": []string{"OPEN"},
	}
	if params.Branch != "" {
		variables["branch"] = params.Branch
		variables["states"] = []string{"OPEN", "CLOSED", "MERGED"}
	}

	var data struct {
		Repository *struct {
			PullRequests connection `json:"pullRequests"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, listQuery, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}

	return graphql.PageOf(&data.Repository.PullRequests, answer.RateLimit, func(node pullRequestNode) forge.PullRequest {
		return node.toPullRequest(false)
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
	if err := failureOf(answer); err != nil {
		return forge.PullRequest{}, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return forge.PullRequest{}, noPullRequest(number, owner, name)
	}

	return data.Repository.PullRequest.toPullRequest(false), nil
}

// branchesPerQuery is how many branches one ReadBranches query asks about.
const branchesPerQuery = 50

// pullRequestsPerBranch is how many of a branch's newest pull requests ReadBranches reads.
const pullRequestsPerBranch = 5

// ReadBranches reads the newest pull requests opened from each branch, with their standing,
// a batch of branches to a query by alias, keeping only those whose head is the repository itself.
func (f *Forge) ReadBranches(ctx context.Context, repo git.RemoteURL, branches []string) (*forge.Reading, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}

	reading := &forge.Reading{}
	for batch := range slices.Chunk(branches, branchesPerQuery) {
		read, err := f.readBranches(ctx, owner, name, batch)
		if err != nil {
			return nil, err
		}
		reading.RateLimit = read.RateLimit
		reading.PullRequests = append(reading.PullRequests, read.PullRequests...)
	}

	return reading, nil
}

// readBranches is what one query answers about a batch of branches.
func (f *Forge) readBranches(ctx context.Context, owner, name string, branches []string) (*forge.Reading, error) {
	variables := map[string]any{"owner": owner, "name": name}
	var declared, fields strings.Builder
	for i, branch := range branches {
		variables["b"+strconv.Itoa(i)] = branch
		fmt.Fprintf(&declared, ", $b%d: String!", i)
		fmt.Fprintf(&fields, "\n    b%d: pullRequests(headRefName: $b%d, first: %d, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { ...pullRequestFields %s } }", i, i, pullRequestsPerBranch, standingFields)
	}
	operation := "query ReadBranches($owner: String!, $name: String!" + declared.String() + ") {\n  repository(owner: $owner, name: $name) {" + fields.String() + "\n  }\n}\n" + pullRequestFragment

	var data struct {
		Repository map[string]*struct {
			Nodes []pullRequestNode `json:"nodes"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}

	reading := &forge.Reading{RateLimit: answer.RateLimit}
	for i := range branches {
		answered := data.Repository["b"+strconv.Itoa(i)]
		if answered == nil {
			continue
		}

		for _, node := range answered.Nodes {
			pullRequest := node.toPullRequest(true)
			if !pullRequest.IsFromItsOwnRepository() {
				continue
			}

			reading.PullRequests = append(reading.PullRequests, pullRequest)
		}
	}

	return reading, nil
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
				ID    string `json:"id"`
				State string `json:"state"`
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
	if err := failureOf(answer); err != nil {
		return err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return noPullRequest(number, owner, name)
	}
	switch data.Repository.PullRequest.State {
	case "MERGED":
		return forge.ErrNotMergeable.Saying("github will not merge #%d: it has already merged", number)
	case "CLOSED":
		return forge.ErrNotMergeable.Saying("github will not merge #%d: it was closed without merging", number)
	}

	var merged struct{}
	answer, err = f.client.Query(ctx, mergeMutation, map[string]any{
		"id":     data.Repository.PullRequest.ID,
		"method": data.Repository.ViewerDefaultMergeMethod,
	}, &merged)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotMergeable, verb: "merge"}.explain(answer, address, number)
}

// SetDraft converts the pull request to a draft or marks it ready for review, asking nothing
// of GitHub when it already stands that way.
func (f *Forge) SetDraft(ctx context.Context, address string, draft bool) error {
	owner, name, number, err := parsePullURL(address)
	if err != nil {
		return err
	}

	var data struct {
		Repository *struct {
			PullRequest *struct {
				ID      string `json:"id"`
				State   string `json:"state"`
				IsDraft bool   `json:"isDraft"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, draftTargetQuery, map[string]any{
		"owner":  owner,
		"name":   name,
		"number": number,
	}, &data)
	if err != nil {
		return err
	}
	if err := failureOf(answer); err != nil {
		return err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return noPullRequest(number, owner, name)
	}
	target := data.Repository.PullRequest
	switch target.State {
	case "MERGED":
		return forge.ErrNotDraftable.Saying("github will not change #%d: it has already merged", number)
	case "CLOSED":
		return forge.ErrNotDraftable.Saying("github will not change #%d: it was closed", number)
	}
	if target.IsDraft == draft {
		return nil
	}

	mutation := markReadyMutation
	if draft {
		mutation = convertToDraftMutation
	}

	var changed struct{}
	answer, err = f.client.Query(ctx, mutation, map[string]any{"id": target.ID}, &changed)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNotDraftable, verb: "change"}.explain(answer, address, number)
}

// SetAutoMerge asks GitHub to merge the pull request with the credential's default method once
// it may, holding it to the head the person saw, or withdraws that request, asking nothing of
// GitHub when it already stands that way.
func (f *Forge) SetAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	owner, name, number, err := parsePullURL(params.URL)
	if err != nil {
		return err
	}

	target, err := f.readAutoMergeTarget(ctx, readAutoMergeTargetParams{
		owner:  owner,
		name:   name,
		number: number,
	})
	if err != nil {
		return err
	}
	switch target.State {
	case "MERGED":
		return forge.ErrNoAutoMerge.Saying("github will not change #%d: it has already merged", number)
	case "CLOSED":
		return forge.ErrNoAutoMerge.Saying("github will not change #%d: it was closed", number)
	}
	if (target.AutoMergeRequest != nil) == params.On {
		return nil
	}

	mutation := disableAutoMergeMutation
	variables := map[string]any{"id": target.ID}
	if params.On {
		mutation = enableAutoMergeMutation
		variables["method"] = target.method
		variables["head"] = xptr.NonEmpty(params.HeadSHA)
	}

	var changed struct{}
	answer, err := f.client.Query(ctx, mutation, variables, &changed)
	if err != nil {
		return err
	}

	return mutationRefusal{sentinel: forge.ErrNoAutoMerge, verb: "change auto-merge on"}.explain(answer, params.URL, number)
}

// readAutoMergeTargetParams names the pull request whose auto-merge is read.
type readAutoMergeTargetParams struct {
	owner  string
	name   string
	number int
}

// readAutoMergeTarget reads the pull request whose auto-merge is about to change.
func (f *Forge) readAutoMergeTarget(ctx context.Context, params readAutoMergeTargetParams) (*autoMergeTarget, error) {
	var data struct {
		Repository *struct {
			ViewerDefaultMergeMethod string           `json:"viewerDefaultMergeMethod"`
			PullRequest              *autoMergeTarget `json:"pullRequest"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, autoMergeTargetQuery, map[string]any{
		"owner":  params.owner,
		"name":   params.name,
		"number": params.number,
	}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.PullRequest == nil {
		return nil, noPullRequest(params.number, params.owner, params.name)
	}

	target := data.Repository.PullRequest
	target.method = data.Repository.ViewerDefaultMergeMethod

	return target, nil
}

// noRepository is GitHub answering null for the repository a query's variables name.
func noRepository(variables map[string]any) error {
	return fmt.Errorf("%w: github has no repository %s/%s, which is also its answer for one the credential cannot see", forge.ErrNotFound, variables["owner"], variables["name"])
}

func noPullRequest(number int, owner, name string) error {
	return fmt.Errorf("%w: github has no pull request %d in %s/%s", forge.ErrNotFound, number, owner, name)
}

type mutationRefusal struct {
	sentinel *fail.Error
	verb     string
}

func (r mutationRefusal) explain(answer *graphql.Answer, address string, number int) error {
	for _, failure := range answer.Errors {
		switch failure.Type {
		case "UNPROCESSABLE":
			return r.sentinel.Saying("github will not %s #%d: %s", r.verb, number, failure.Message)
		case "FORBIDDEN":
			return forge.ErrUnauthorized.Saying("github will not let the credential %s %s: %s", r.verb, address, failure.Message)
		}
	}

	return failureOf(answer)
}

// failureOf is the first error the answer carries as komrad's, nil when it carries none.
func failureOf(answer *graphql.Answer) error {
	if len(answer.Errors) == 0 {
		return nil
	}

	failure := answer.Errors[0]
	if failure.Type == "NOT_FOUND" {
		return fmt.Errorf("%w: %s, which is also github's answer for a repository the credential cannot see", forge.ErrNotFound, failure.Message)
	}

	return fmt.Errorf("github refused the query: %s", failure.Message)
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

// autoMergeTarget is a pull request whose auto-merge is about to change, with the method the
// credential merges with by default.
type autoMergeTarget struct {
	ID               string    `json:"id"`
	State            string    `json:"state"`
	AutoMergeRequest *struct{} `json:"autoMergeRequest"`
	method           string
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
	AutoMergeRequest         *struct{} `json:"autoMergeRequest"`
	ViewerCanEnableAutoMerge bool      `json:"viewerCanEnableAutoMerge"`
}

type rollupNode struct {
	State string `json:"state"`
}

// autoMerge is whether the pull request carries an auto-merge request and whether the
// credential may make one, which GitHub refuses on a draft, a pull request that can merge now
// and a repository that does not allow it.
func (n standingNode) autoMerge() forge.AutoMerge {
	return forge.AutoMerge{
		On:      n.AutoMergeRequest != nil,
		Allowed: n.ViewerCanEnableAutoMerge,
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
		pullRequest.AutoMerge = xptr.Ptr(n.autoMerge())
	}

	return pullRequest
}

// Build is the Builder for GitHub.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return NewForge(NewForgeParams{Auth: auth, API: api})
}

// Package gitlab is the forge behind gitlab.com and self-hosted GitLab, where a pull request is
// a merge request.
package gitlab

import (
	"context"
	"encoding/json"
	"errors"
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

// DefaultAPI is where gitlab.com answers REST calls, with GraphQL beside it.
const DefaultAPI = "https://gitlab.com/api/v4"

// MaxPerPage is the most GitLab hands over in one page, above which it silently trims.
const MaxPerPage = 100

// changedProjectsPerQuery is how many projects the first ListChanged query aliases, the
// measured fit under GitLab's complexity limit at a hundred rows each.
const changedProjectsPerQuery = 2

// standingsPerQuery is how many merge requests the first Standings query aliases, the
// measured fit under GitLab's complexity limit.
const standingsPerQuery = 10

// reviewStateRequestedChanges is the reviewer state GitLab records when one asks for changes.
const reviewStateRequestedChanges = "REQUESTED_CHANGES"

// mergeRequestFragment is every field of a merge request the record is built from.
const mergeRequestFragment = `fragment mergeRequestFields on MergeRequest {
  id iid title webUrl state draft createdAt updatedAt mergedAt closedAt
  sourceBranch targetBranch diffHeadSha sourceProject { webUrl } targetProject { webUrl } diffRefs { baseSha }
}`

// standingFields is what the decision, checks and mergeability of a merge request are folded
// from: its approvals, its reviewers' states, the status of the pipeline on its head and its
// detailed merge status.
const standingFields = `approvalsRequired approvalsLeft approvedBy { count } reviewers { nodes { mergeRequestInteraction { reviewState } } } headPipeline { status } detailedMergeStatus`

const verifyQuery = `query Verify { currentUser { username } }`

const getQuery = `query Get($path: ID!, $iid: String!) {
  project(fullPath: $path) { mergeRequest(iid: $iid) { ...mergeRequestFields } }
}
` + mergeRequestFragment

const listQuery = `query List($path: ID!, $first: Int!, $after: String, $branches: [String!], $state: MergeRequestState) {
  project(fullPath: $path) {
    mergeRequests(sourceBranches: $branches, state: $state, sort: CREATED_DESC, first: $first, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { ...mergeRequestFields }
    }
  }
}
` + mergeRequestFragment

const listUpdatedQuery = `query ListUpdated($path: ID!, $first: Int!, $after: String) {
  project(fullPath: $path) {
    mergeRequests(sort: UPDATED_DESC, first: $first, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { ...mergeRequestFields }
    }
  }
}
` + mergeRequestFragment

// errTooComplex is GitLab refusing a query for its complexity.
var errTooComplex = fail.Exhausted("gitlab refused the query for its complexity")

// errNotCursor is a ListChanged cursor this forge did not hand out.
var errNotCursor = fail.InvalidArgument("not a gitlab cursor")

// dialect is what GitLab does differently on the wire.
var dialect = graphql.Dialect{
	Name:            "gitlab",
	RateLimitHeader: "RateLimit",
	Version:         "/v4",
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

// Forge is GitLab, spoken to over its GraphQL API.
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
	return forge.KindGitLab
}

// Verify asks GitLab who the credential belongs to.
func (f *Forge) Verify(ctx context.Context) (string, error) {
	var data struct {
		CurrentUser *struct {
			Username string `json:"username"`
		} `json:"currentUser"`
	}
	answer, err := f.client.Query(ctx, verifyQuery, nil, &data)
	if err != nil {
		return "", err
	}
	if err := failureOf(answer); err != nil {
		return "", err
	}
	if data.CurrentUser == nil || data.CurrentUser.Username == "" {
		return "", fmt.Errorf("%w: gitlab answered currentUser with nobody", forge.ErrUnauthorized)
	}

	return data.CurrentUser.Username, nil
}

// List is one page of the project's merge requests, every one from the named branch or the
// open ones when no branch is named, and where the next page starts.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.Page, error) {
	path, err := projectPath(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{
		"path":  path,
		"first": min(max(params.PerPage, 1), MaxPerPage),
		"after": graphql.CursorOf(params.Cursor),
		"state": "opened",
	}
	if params.Branch != "" {
		variables["branches"] = []string{params.Branch}
		delete(variables, "state")
	}

	return f.listMergeRequests(ctx, listQuery, variables)
}

// ListUpdated is one page of every merge request in the project, newest change first, and
// where the next page starts.
func (f *Forge) ListUpdated(ctx context.Context, params forge.ListUpdatedParams) (*forge.Page, error) {
	path, err := projectPath(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	return f.listMergeRequests(ctx, listUpdatedQuery, map[string]any{
		"path":  path,
		"first": MaxPerPage,
		"after": graphql.CursorOf(params.Cursor),
	})
}

// ListChanged is one page of every merge request across the projects changed at or after
// Since, newest first per project, each with its decision, read with the projects aliased
// into as few queries as GitLab's complexity limit allows.
func (f *Forge) ListChanged(ctx context.Context, params forge.ListChangedParams) (*forge.Page, error) {
	pending := map[string]string{}
	if params.Cursor != "" {
		if err := json.Unmarshal([]byte(params.Cursor), &pending); err != nil {
			return nil, errNotCursor.Saying("%q is not a cursor this forge handed out", params.Cursor)
		}
	} else {
		for _, repo := range params.Repos {
			path, err := projectPath(repo.Path)
			if err != nil {
				return nil, err
			}

			pending[path] = ""
		}
	}

	paths := make([]string, 0, len(pending))
	for path := range pending {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	result := &forge.Page{}
	next := map[string]string{}
	err := sendInBatches(ctx, paths, changedProjectsPerQuery, func(ctx context.Context, batch []string) (complexity, error) {
		read, err := f.listChangedBatch(ctx, listChangedBatchParams{
			paths: batch,
			after: pending,
			since: params.Since,
		})
		if err != nil {
			return complexity{}, err
		}

		for i, path := range batch {
			page := read.pages[i]
			if page == nil {
				continue
			}

			result.PullRequests = append(result.PullRequests, page.PullRequests...)
			if page.HasMore {
				next[path] = page.Cursor
			}
		}
		result.RateLimit = read.rateLimit

		return read.complexity, nil
	})
	if err != nil {
		return nil, err
	}

	if len(next) > 0 {
		cursor, err := json.Marshal(next)
		if err != nil {
			return nil, fmt.Errorf("failed to encode the cursor: %w", err)
		}

		result.HasMore = true
		result.Cursor = string(cursor)
	}

	return result, nil
}

// listChangedBatchParams says which projects one ListChanged query reads, each from its own cursor.
type listChangedBatchParams struct {
	paths []string
	after map[string]string
	since time.Time
}

// changedBatch is what one ListChanged query answered, a page per project asked about.
type changedBatch struct {
	pages      []*forge.Page
	rateLimit  forge.RateLimit
	complexity complexity
}

// listChangedBatch reads one page of changes from each project in one query, answering nil
// for a project GitLab has nothing for.
func (f *Forge) listChangedBatch(ctx context.Context, params listChangedBatchParams) (*changedBatch, error) {
	var declared, fields strings.Builder
	variables := map[string]any{"since": params.since.UTC().Format(time.RFC3339), "first": MaxPerPage}
	for i, path := range params.paths {
		fmt.Fprintf(&declared, ", $p%d: ID!, $a%d: String", i, i)
		fmt.Fprintf(&fields, "\n  r%d: project(fullPath: $p%d) { mergeRequests(updatedAfter: $since, sort: UPDATED_DESC, first: $first, after: $a%d) { pageInfo { hasNextPage endCursor } nodes { ...mergeRequestFields "+standingFields+" } } }", i, i, i)
		variables["p"+strconv.Itoa(i)] = path
		variables["a"+strconv.Itoa(i)] = graphql.CursorOf(params.after[path])
	}
	operation := "query ListChanged($since: Time, $first: Int!" + declared.String() + ") {" + fields.String() + "\n  queryComplexity { score limit }\n}\n" + mergeRequestFragment

	var data map[string]json.RawMessage
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}

	read := &changedBatch{
		pages:      make([]*forge.Page, len(params.paths)),
		rateLimit:  answer.RateLimit,
		complexity: complexityOf(data),
	}
	for i, path := range params.paths {
		var project *struct {
			MergeRequests connection `json:"mergeRequests"`
		}
		if err := json.Unmarshal(data["r"+strconv.Itoa(i)], &project); err != nil {
			return nil, fmt.Errorf("failed to decode gitlab's answer for %s: %w", path, err)
		}
		if project != nil {
			read.pages[i] = pageOf(&project.MergeRequests, answer.RateLimit, true)
		}
	}

	return read, nil
}

// Get is one merge request by its web address.
func (f *Forge) Get(ctx context.Context, address string) (forge.PullRequest, error) {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return forge.PullRequest{}, err
	}

	var data struct {
		Project *struct {
			MergeRequest *mergeRequestNode `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, getQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if err := failureOf(answer); err != nil {
		return forge.PullRequest{}, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return forge.PullRequest{}, fmt.Errorf("%w: gitlab has no merge request %d in %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, number, path)
	}

	return data.Project.MergeRequest.toPullRequest(false), nil
}

// Merge accepts the merge request the way the project is set to merge.
func (f *Forge) Merge(ctx context.Context, address string) error {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return err
	}

	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPut,
		URL:    fmt.Sprintf("%s/projects/%s/merge_requests/%d/merge", f.client.API(), url.PathEscape(path), number),
	}, nil)
	if errors.Is(err, forge.ErrThrottled) {
		return err
	}

	switch answer.Status {
	case http.StatusNotFound:
		return fmt.Errorf("%w: gitlab has no merge request %d in %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, number, path)
	case http.StatusForbidden:
		return forge.ErrUnauthorized.Saying("gitlab will not let the credential merge %s: %v", address, err)
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusConflict, http.StatusUnprocessableEntity:
		return forge.ErrNotMergeable.Saying("gitlab refused to merge %s: %v", address, err)
	}

	return err
}

// Standings reads where the reviews and the head pipeline of up to fifty merge requests stand,
// aliased into as few queries as GitLab's complexity limit allows, leaving out any GitLab does
// not have.
func (f *Forge) Standings(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]forge.Standing, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}
	if len(numbers) > forge.MaxStandings {
		return nil, forge.ErrTooManyStandings.Saying("at most %d standings can be read in one call, not %d", forge.MaxStandings, len(numbers))
	}

	standings := make(map[int]forge.Standing, len(numbers))
	err = sendInBatches(ctx, numbers, standingsPerQuery, func(ctx context.Context, batch []int) (complexity, error) {
		var fields strings.Builder
		for _, number := range batch {
			fmt.Fprintf(&fields, "\n    m%d: mergeRequest(iid: \"%d\") { diffHeadSha %s }", number, number, standingFields)
		}
		operation := "query Standings($path: ID!) {\n  project(fullPath: $path) {" + fields.String() + "\n  }\n  queryComplexity { score limit }\n}"

		var data struct {
			Project         map[string]*standingNode `json:"project"`
			QueryComplexity complexity               `json:"queryComplexity"`
		}
		answer, err := f.client.Query(ctx, operation, map[string]any{"path": path}, &data)
		if err != nil {
			return complexity{}, err
		}
		if err := failureOf(answer); err != nil {
			return complexity{}, err
		}
		if data.Project == nil {
			return complexity{}, fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, path)
		}

		for _, number := range batch {
			node := data.Project["m"+strconv.Itoa(number)]
			if node == nil {
				continue
			}

			standings[number] = node.standing()
		}

		return data.QueryComplexity, nil
	})
	if err != nil {
		return nil, err
	}

	return standings, nil
}

// listMergeRequests reads one page of a project's mergeRequests connection, whichever query
// shapes it.
func (f *Forge) listMergeRequests(ctx context.Context, operation string, variables map[string]any) (*forge.Page, error) {
	var data struct {
		Project *struct {
			MergeRequests connection `json:"mergeRequests"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil {
		return nil, fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, variables["path"])
	}

	return pageOf(&data.Project.MergeRequests, answer.RateLimit, false), nil
}

// pageOf is a connection as one page of pull requests, each with its standing when asked for.
func pageOf(c *connection, limit forge.RateLimit, withStanding bool) *forge.Page {
	return graphql.PageOf(c, limit, func(node mergeRequestNode) forge.PullRequest {
		return node.toPullRequest(withStanding)
	})
}

// sendInBatches sends one query per batch of items, sized from what GitLab last said about its
// complexity budget, halved when it refuses a batch for exceeding it and never again as large.
func sendInBatches[T any](ctx context.Context, items []T, size int, send func(context.Context, []T) (complexity, error)) error {
	ceiling := len(items)
	for len(items) > 0 {
		count := min(size, ceiling, len(items))
		measured, err := send(ctx, items[:count])
		if errors.Is(err, errTooComplex) && count > 1 {
			ceiling = count - 1
			size = count / 2

			continue
		}
		if err != nil {
			return err
		}

		items = items[count:]
		if measured.Score > 0 && measured.Limit > 0 {
			size = max(1, count*measured.Limit/measured.Score)
		}
	}

	return nil
}

// complexity is what GitLab said one query cost against its limit.
type complexity struct {
	Score int `json:"score"`
	Limit int `json:"limit"`
}

// complexityOf reads the queryComplexity field out of an answer's data, zero when it is not
// there.
func complexityOf(data map[string]json.RawMessage) complexity {
	var measured complexity
	if err := json.Unmarshal(data["queryComplexity"], &measured); err != nil {
		return complexity{}
	}

	return measured
}

// failureOf is the first error the answer carries as komrad's, nil when it carries none.
func failureOf(answer *graphql.Answer) error {
	if len(answer.Errors) == 0 {
		return nil
	}

	failure := answer.Errors[0]
	if strings.Contains(failure.Message, "exceeds max complexity") {
		return fmt.Errorf("%w: %s", errTooComplex, failure.Message)
	}

	return fmt.Errorf("gitlab refused the query: %s", failure.Message)
}

// projectPath is the full path a project is addressed by.
func projectPath(path string) (string, error) {
	if path == "" || !strings.Contains(path, "/") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "", fmt.Errorf("%w: %q is not group/project", forge.ErrNotRepository, path)
	}

	return path, nil
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

// connection is one page of a merge request connection as GitLab answers it.
type connection = graphql.Connection[mergeRequestNode]

// mergeRequestNode is the slice of GitLab's merge request node the record is built from.
type mergeRequestNode struct {
	standingNode
	ID            string       `json:"id"`
	IID           string       `json:"iid"`
	Title         string       `json:"title"`
	WebURL        string       `json:"webUrl"`
	State         string       `json:"state"`
	Draft         bool         `json:"draft"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
	MergedAt      *time.Time   `json:"mergedAt"`
	ClosedAt      *time.Time   `json:"closedAt"`
	SourceBranch  string       `json:"sourceBranch"`
	TargetBranch  string       `json:"targetBranch"`
	SourceProject *projectNode `json:"sourceProject"`
	TargetProject *projectNode `json:"targetProject"`
	DiffRefs      *struct {
		BaseSha string `json:"baseSha"`
	} `json:"diffRefs"`
}

type projectNode struct {
	WebURL string `json:"webUrl"`
}

// remote is the project the node names, zero for one GitLab no longer has.
func (n *projectNode) remote() git.RemoteURL {
	if n == nil {
		return git.RemoteURL{}
	}

	return graphql.RemoteOf(n.WebURL)
}

// standingNode is what GitLab says about a merge request's reviews and the pipeline on its
// head.
type standingNode struct {
	DiffHeadSha       string `json:"diffHeadSha"`
	ApprovalsRequired int    `json:"approvalsRequired"`
	ApprovalsLeft     int    `json:"approvalsLeft"`
	ApprovedBy        struct {
		Count int `json:"count"`
	} `json:"approvedBy"`
	Reviewers struct {
		Nodes []struct {
			MergeRequestInteraction *struct {
				ReviewState string `json:"reviewState"`
			} `json:"mergeRequestInteraction"`
		} `json:"nodes"`
	} `json:"reviewers"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"headPipeline"`
	DetailedMergeStatus string `json:"detailedMergeStatus"`
}

// standing is the node's decision, checks and mergeability with the head they were read for.
func (n standingNode) standing() forge.Standing {
	return forge.Standing{
		Decision:     n.decision(),
		Checks:       n.checks(),
		Mergeability: n.mergeability(),
		HeadSHA:      n.DiffHeadSha,
	}
}

// mergeability is the detailed merge status in komrad's words.
func (n standingNode) mergeability() forge.Mergeability {
	switch n.DetailedMergeStatus {
	case "":
		return forge.MergeabilityUnknown
	case "MERGEABLE":
		return forge.MergeabilityClean
	case "CI_MUST_PASS", "CI_STILL_RUNNING":
		return forge.MergeabilityUnstable
	case "NEED_REBASE":
		return forge.MergeabilityBehind
	case "CONFLICT":
		return forge.MergeabilityConflicting
	case "UNCHECKED", "CHECKING", "PREPARING", "APPROVALS_SYNCING":
		return forge.MergeabilityUnknown
	}

	return forge.MergeabilityBlocked
}

// checks is the head pipeline's status in komrad's words.
func (n standingNode) checks() forge.Checks {
	if n.HeadPipeline == nil {
		return forge.ChecksNone
	}

	switch n.HeadPipeline.Status {
	case "SUCCESS":
		return forge.ChecksPassing
	case "FAILED", "CANCELED", "CANCELING":
		return forge.ChecksFailing
	case "CREATED", "WAITING_FOR_RESOURCE", "PREPARING", "WAITING_FOR_CALLBACK", "PENDING", "RUNNING", "SCHEDULED", "MANUAL":
		return forge.ChecksPending
	}

	return forge.ChecksNone
}

// decision folds the approvals into komrad's word, a change request first, then approvals
// still owed, then an approval given.
func (n standingNode) decision() forge.ReviewDecision {
	for _, reviewer := range n.Reviewers.Nodes {
		if reviewer.MergeRequestInteraction != nil && reviewer.MergeRequestInteraction.ReviewState == reviewStateRequestedChanges {
			return forge.ReviewDecisionChangesRequested
		}
	}

	switch {
	case n.ApprovalsLeft > 0:
		return forge.ReviewDecisionPending
	case n.ApprovedBy.Count > 0:
		return forge.ReviewDecisionApproved
	}

	return forge.ReviewDecisionNone
}

// toPullRequest is the node as the record, carrying its decision and checks when the query
// asked for them.
func (n mergeRequestNode) toPullRequest(withStanding bool) forge.PullRequest {
	baseSHA := ""
	if n.DiffRefs != nil {
		baseSHA = n.DiffRefs.BaseSha
	}

	pullRequest := forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: strings.TrimPrefix(n.ID, "gid://gitlab/MergeRequest/"),
		URL:        n.WebURL,
		Repository: n.TargetProject.remote(),
		Head:       n.SourceProject.remote(),
		Number:     numberOf(n.IID),
		Title:      n.Title,
		HeadBranch: n.SourceBranch,
		HeadSHA:    n.DiffHeadSha,
		BaseBranch: n.TargetBranch,
		BaseSHA:    baseSHA,
		Draft:      n.Draft,
		State:      stateOf(n.State),
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		MergedAt:   n.MergedAt,
		ClosedAt:   n.closedAt(),
	}
	if withStanding {
		pullRequest.Decision = xptr.Ptr(n.decision())
		pullRequest.Checks = xptr.Ptr(n.checks())
		pullRequest.Mergeability = xptr.Ptr(n.mergeability())
	}

	return pullRequest
}

// numberOf is the iid as a number.
func numberOf(iid string) int {
	number, err := strconv.Atoi(iid)
	if err != nil {
		return 0
	}

	return number
}

// closedAt is when the merge request finished, which GitLab stamps only on a close and komrad
// records on a merge as well.
func (n mergeRequestNode) closedAt() *time.Time {
	if n.ClosedAt != nil {
		return n.ClosedAt
	}

	return n.MergedAt
}

// stateOf is GitLab's state in the words GitHub's enum uses.
func stateOf(state string) string {
	if state == "opened" {
		return "OPEN"
	}

	return strings.ToUpper(state)
}

// Build is the Builder for GitLab.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return NewForge(NewForgeParams{Auth: auth, API: api})
}

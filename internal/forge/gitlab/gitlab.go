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

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/lib/xhttp"
	"weave/lib/xptr"
)

// DefaultAPI is where gitlab.com answers REST calls, with GraphQL beside it.
const DefaultAPI = "https://gitlab.com/api/v4"

// DefaultTimeout bounds one call to GitLab.
const DefaultTimeout = 30 * time.Second

// MaxPerPage is the most GitLab hands over in one page, above which it silently trims.
const MaxPerPage = 100

// changedProjectsPerQuery is how many projects the first ListChanged query aliases, the
// measured fit under GitLab's complexity limit at a hundred rows each.
const changedProjectsPerQuery = 2

// decisionsPerQuery is how many merge requests the first ReviewDecisions query aliases, the
// measured fit under GitLab's complexity limit.
const decisionsPerQuery = 13

// reviewStateRequestedChanges is the reviewer state GitLab records when one asks for changes.
const reviewStateRequestedChanges = "REQUESTED_CHANGES"

// mergeRequestFragment is every field of a merge request the record is built from.
const mergeRequestFragment = `fragment mergeRequestFields on MergeRequest {
  id iid title webUrl state draft createdAt updatedAt mergedAt closedAt
  sourceBranch targetBranch diffHeadSha sourceProject { webUrl } targetProject { webUrl } diffRefs { baseSha }
}`

// approvalFields is what the decision of a merge request is folded from.
const approvalFields = `approvalsRequired approvalsLeft approvedBy { count } reviewers { nodes { mergeRequestInteraction { reviewState } } }`

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

// Forge is GitLab, spoken to over its GraphQL API.
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
	var data struct {
		CurrentUser *struct {
			Username string `json:"username"`
		} `json:"currentUser"`
	}
	answer, err := f.query(ctx, verifyQuery, nil, &data)
	if err != nil {
		return "", err
	}
	if err := answer.failure(); err != nil {
		return "", err
	}
	if data.CurrentUser == nil || data.CurrentUser.Username == "" {
		return "", fmt.Errorf("%w: gitlab answered currentUser with nobody", forge.ErrUnauthorized)
	}

	return data.CurrentUser.Username, nil
}

// List is one page of the project's merge requests, every one from the named branch or the
// open ones when no branch is named, and where the next page starts.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.ListResult, error) {
	path, err := projectPath(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{
		"path":  path,
		"first": min(max(params.PerPage, 1), MaxPerPage),
		"after": cursorOf(params.Cursor),
		"state": "opened",
	}
	if params.Branch != "" {
		variables["branches"] = []string{params.Branch}
		delete(variables, "state")
	}

	page, limit, err := f.listMergeRequests(ctx, listQuery, variables, path)
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

// ListUpdated is one page of every merge request in the project, newest change first, and
// where the next page starts.
func (f *Forge) ListUpdated(ctx context.Context, params forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	path, err := projectPath(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	page, limit, err := f.listMergeRequests(ctx, listUpdatedQuery, map[string]any{
		"path":  path,
		"first": MaxPerPage,
		"after": cursorOf(params.Cursor),
	}, path)
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

// ListChanged is one page of every merge request across the projects changed at or after
// Since, newest first per project, each with its decision, read with the projects aliased
// into as few queries as GitLab's complexity limit allows.
func (f *Forge) ListChanged(ctx context.Context, params forge.ListChangedParams) (*forge.ListChangedResult, error) {
	pending := map[string]string{}
	if params.Cursor != "" {
		if err := json.Unmarshal([]byte(params.Cursor), &pending); err != nil {
			return nil, fail.InvalidArgument("not a gitlab cursor").Saying("%q is not a cursor this forge handed out", params.Cursor)
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

	result := &forge.ListChangedResult{}
	next := map[string]string{}
	err := sendInBatches(ctx, paths, changedProjectsPerQuery, func(ctx context.Context, batch []string) (complexity, error) {
		answer, pages, err := f.listChangedBatch(ctx, batch, pending, params.Since)
		if err != nil {
			return complexity{}, err
		}

		for i, path := range batch {
			page := pages[i]
			if page == nil {
				continue
			}

			result.PullRequests = append(result.PullRequests, page.toPullRequests(true)...)
			if page.PageInfo.HasNextPage {
				next[path] = page.PageInfo.EndCursor
			}
		}
		result.RateLimit = answer.rateLimit

		return answer.complexity, nil
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

// listChangedBatch reads one page of changes from each project in one query, answering nil
// for a project GitLab has nothing for.
func (f *Forge) listChangedBatch(ctx context.Context, paths []string, after map[string]string, since time.Time) (*graphqlAnswer, []*connection, error) {
	var declared, fields strings.Builder
	variables := map[string]any{"since": since.UTC().Format(time.RFC3339), "first": MaxPerPage}
	for i, path := range paths {
		fmt.Fprintf(&declared, ", $p%d: ID!, $a%d: String", i, i)
		fmt.Fprintf(&fields, "\n  r%d: project(fullPath: $p%d) { mergeRequests(updatedAfter: $since, sort: UPDATED_DESC, first: $first, after: $a%d) { pageInfo { hasNextPage endCursor } nodes { ...mergeRequestFields "+approvalFields+" } } }", i, i, i)
		variables["p"+strconv.Itoa(i)] = path
		variables["a"+strconv.Itoa(i)] = cursorOf(after[path])
	}
	operation := "query ListChanged($since: Time, $first: Int!" + declared.String() + ") {" + fields.String() + "\n  queryComplexity { score limit }\n}\n" + mergeRequestFragment

	var data map[string]json.RawMessage
	answer, err := f.query(ctx, operation, variables, &data)
	if err != nil {
		return nil, nil, err
	}
	if err := answer.failure(); err != nil {
		return nil, nil, err
	}
	answer.complexity = complexityOf(data)

	pages := make([]*connection, len(paths))
	for i := range paths {
		var project *struct {
			MergeRequests connection `json:"mergeRequests"`
		}
		if err := json.Unmarshal(data["r"+strconv.Itoa(i)], &project); err != nil {
			return nil, nil, fmt.Errorf("failed to decode gitlab's answer for %s: %w", paths[i], err)
		}
		if project != nil {
			pages[i] = &project.MergeRequests
		}
	}

	return answer, pages, nil
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
	answer, err := f.query(ctx, getQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if err := answer.failure(); err != nil {
		return forge.PullRequest{}, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return forge.PullRequest{}, fmt.Errorf("%w: gitlab has no merge request %d in %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, number, path)
	}

	return data.Project.MergeRequest.toPullRequest(false), nil
}

// ReviewDecisions reads where the reviews of up to fifty merge requests stand, aliased into as
// few queries as GitLab's complexity limit allows, leaving out any GitLab does not have.
func (f *Forge) ReviewDecisions(ctx context.Context, repo git.RemoteURL, numbers []int) (map[int]forge.ReviewDecision, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}
	if len(numbers) > forge.MaxReviewDecisions {
		return nil, fail.InvalidArgument("too many review decisions for one call").Saying("at most %d review decisions can be read in one call, not %d", forge.MaxReviewDecisions, len(numbers))
	}

	decisions := make(map[int]forge.ReviewDecision, len(numbers))
	err = sendInBatches(ctx, numbers, decisionsPerQuery, func(ctx context.Context, batch []int) (complexity, error) {
		var fields strings.Builder
		for _, number := range batch {
			fmt.Fprintf(&fields, "\n    m%d: mergeRequest(iid: \"%d\") { %s }", number, number, approvalFields)
		}
		operation := "query ReviewDecisions($path: ID!) {\n  project(fullPath: $path) {" + fields.String() + "\n  }\n  queryComplexity { score limit }\n}"

		var data struct {
			Project         map[string]*approvalNode `json:"project"`
			QueryComplexity complexity               `json:"queryComplexity"`
		}
		answer, err := f.query(ctx, operation, map[string]any{"path": path}, &data)
		if err != nil {
			return complexity{}, err
		}
		if err := answer.failure(); err != nil {
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

			decisions[number] = node.decision()
		}

		return data.QueryComplexity, nil
	})
	if err != nil {
		return nil, err
	}

	return decisions, nil
}

// listMergeRequests reads one page of a project's mergeRequests connection, whichever query
// shapes it.
func (f *Forge) listMergeRequests(ctx context.Context, operation string, variables map[string]any, path string) (*connection, forge.RateLimit, error) {
	var data struct {
		Project *struct {
			MergeRequests connection `json:"mergeRequests"`
		} `json:"project"`
	}
	answer, err := f.query(ctx, operation, variables, &data)
	if err != nil {
		return nil, forge.RateLimit{}, err
	}
	if err := answer.failure(); err != nil {
		return nil, forge.RateLimit{}, err
	}
	if data.Project == nil {
		return nil, forge.RateLimit{}, fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, path)
	}

	return &data.Project.MergeRequests, answer.rateLimit, nil
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

// cursorOf is a cursor as the query variable takes it, null for the first page.
func cursorOf(cursor string) *string {
	if cursor == "" {
		return nil
	}

	return xptr.Ptr(cursor)
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

// graphqlError is one entry of the errors a GraphQL answer carries beside its data.
type graphqlError struct {
	Message string `json:"message"`
}

// graphqlAnswer is what one query came back with besides its data.
type graphqlAnswer struct {
	errors     []graphqlError
	rateLimit  forge.RateLimit
	complexity complexity
}

// failure is the first error the answer carries as weave's, nil when it carries none.
func (a *graphqlAnswer) failure() error {
	if len(a.errors) == 0 {
		return nil
	}

	failure := a.errors[0]
	if strings.Contains(failure.Message, "exceeds max complexity") {
		return fmt.Errorf("%w: %s", errTooComplex, failure.Message)
	}

	return fmt.Errorf("gitlab refused the query: %s", failure.Message)
}

// graphqlURL is where the host answers GraphQL, beside the REST base rather than under it.
func (f *Forge) graphqlURL() string {
	return strings.TrimSuffix(f.api, "/v4") + "/graphql"
}

// query sends one named operation to GitLab's GraphQL endpoint, decodes its data, and turns
// GitLab's refusals into weave's.
func (f *Forge) query(ctx context.Context, operation string, variables map[string]any, into any) (*graphqlAnswer, error) {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphqlError  `json:"errors"`
	}
	answer, err := xhttp.JSON(ctx, xhttp.JSONParams{
		Client: f.client,
		Method: http.MethodPost,
		URL:    f.graphqlURL(),
		Body:   map[string]any{"query": operation, "variables": variables},
	}, &envelope)
	if err != nil {
		if failure := fail.Of(err); failure != nil {
			return nil, failure
		}

		return nil, err
	}

	switch {
	case answer.Status == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s", forge.ErrUnauthorized, messageOf(answer.Body))
	case answer.Status == http.StatusNotFound:
		return nil, fmt.Errorf("gitlab answered 404 at %s, which is not where its GraphQL lives", f.graphqlURL())
	case answer.Status == http.StatusTooManyRequests:
		return nil, throttleOf(answer, f.now())
	case !answer.IsSuccess():
		return nil, fmt.Errorf("gitlab answered %d for %s: %s", answer.Status, operationOf(operation), messageOf(answer.Body))
	}

	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, into); err != nil {
			return nil, fmt.Errorf("failed to decode gitlab's answer to %s: %w", operationOf(operation), err)
		}
	}

	return &graphqlAnswer{errors: envelope.Errors, rateLimit: readRateLimit(answer.Header)}, nil
}

// operationOf is the name a query was written with.
func operationOf(operation string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(operation, "query "), "(")
	name, _, _ = strings.Cut(name, " ")

	return name
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
type connection struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []mergeRequestNode `json:"nodes"`
}

func (c *connection) toPullRequests(withDecision bool) []forge.PullRequest {
	pullRequests := make([]forge.PullRequest, 0, len(c.Nodes))
	for _, node := range c.Nodes {
		pullRequests = append(pullRequests, node.toPullRequest(withDecision))
	}

	return pullRequests
}

// mergeRequestNode is the slice of GitLab's merge request node the record is built from.
type mergeRequestNode struct {
	approvalNode
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
	DiffHeadSha   string       `json:"diffHeadSha"`
	SourceProject *projectNode `json:"sourceProject"`
	TargetProject *projectNode `json:"targetProject"`
	DiffRefs      *struct {
		BaseSha string `json:"baseSha"`
	} `json:"diffRefs"`
}

type projectNode struct {
	WebURL string `json:"webUrl"`
}

// approvalNode is what GitLab says about a merge request's reviews.
type approvalNode struct {
	ApprovalsRequired int `json:"approvalsRequired"`
	ApprovalsLeft     int `json:"approvalsLeft"`
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
}

// decision folds the approvals into weave's word, a change request first, then approvals
// still owed, then an approval given.
func (n approvalNode) decision() forge.ReviewDecision {
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

// toPullRequest is the node as the record, carrying its decision when the query asked for one.
func (n mergeRequestNode) toPullRequest(withDecision bool) forge.PullRequest {
	baseSHA := ""
	if n.DiffRefs != nil {
		baseSHA = n.DiffRefs.BaseSha
	}

	pullRequest := forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: strings.TrimPrefix(n.ID, "gid://gitlab/MergeRequest/"),
		URL:        n.WebURL,
		Repository: remoteOf(n.TargetProject),
		Head:       remoteOf(n.SourceProject),
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
	if withDecision {
		pullRequest.Decision = xptr.Ptr(n.decision())
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

// closedAt is when the merge request finished, which GitLab stamps only on a close and weave
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

// remoteOf is the project a node names, zero for one GitLab no longer has.
func remoteOf(node *projectNode) git.RemoteURL {
	if node == nil {
		return git.RemoteURL{}
	}

	remote, err := git.ParseRemoteURL(node.WebURL)
	if err != nil {
		return git.RemoteURL{}
	}

	return remote
}

// Build is the Builder for GitLab.
func Build(api string, auth forge.Authenticator) forge.Forge {
	return New(NewParams{Auth: auth, API: api})
}

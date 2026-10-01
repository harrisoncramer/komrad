// Package gitlab is the forge behind gitlab.com and self-hosted GitLab, where a pull request is
// a merge request.
package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// branchesPerQuery is how many branches the first ReadBranches query aliases.
const branchesPerQuery = 2

// mergeRequestsPerBranch is how many of a branch's newest merge requests ReadBranches reads.
const mergeRequestsPerBranch = 5

// reviewStateRequestedChanges is the reviewer state GitLab records when one asks for changes.
const reviewStateRequestedChanges = "REQUESTED_CHANGES"

// mergeRequestFragment is every field of a merge request the record is built from.
const mergeRequestFragment = `fragment mergeRequestFields on MergeRequest {
  id iid title webUrl state draft createdAt updatedAt mergedAt closedAt
  sourceBranch targetBranch diffHeadSha sourceProject { webUrl } targetProject { webUrl } diffRefs { baseSha }
}`

// standingFields is what the decision, checks, mergeability and auto-merge of a merge request
// are folded from: its approvals, its reviewers' states, the status of the pipeline on its
// head, its detailed merge status, whether it is set to merge on its own and how it could be.
const standingFields = `approvalsRequired approvalsLeft approvedBy { count } reviewers { nodes { mergeRequestInteraction { reviewState } } } headPipeline { status } detailedMergeStatus autoMergeEnabled availableAutoMergeStrategies`

const verifyQuery = `query Verify { currentUser { username } }`

const getQuery = `query Get($path: ID!, $iid: String!) {
  project(fullPath: $path) { mergeRequest(iid: $iid) { ...mergeRequestFields } }
}
` + mergeRequestFragment

const setDraftMutation = `mutation SetDraft($path: ID!, $iid: String!, $draft: Boolean!) {
  mergeRequestSetDraft(input: {projectPath: $path, iid: $iid, draft: $draft}) { errors }
}`

const autoMergeTargetQuery = `query AutoMergeTarget($path: ID!, $iid: String!) {
  project(fullPath: $path) { mergeRequest(iid: $iid) { state autoMergeEnabled availableAutoMergeStrategies } }
}`

const listQuery = `query List($path: ID!, $first: Int!, $after: String, $branches: [String!], $state: MergeRequestState) {
  project(fullPath: $path) {
    mergeRequests(sourceBranches: $branches, state: $state, sort: CREATED_DESC, first: $first, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { ...mergeRequestFields }
    }
  }
}
` + mergeRequestFragment

// errTooComplex is GitLab refusing a query for its complexity.
var errTooComplex = fail.Exhausted("gitlab refused the query for its complexity")

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
		return forge.PullRequest{}, noMergeRequest(number, path)
	}

	return data.Project.MergeRequest.toPullRequest(false), nil
}

// Merge accepts the merge request the way the project is set to merge.
func (f *Forge) Merge(ctx context.Context, address string) error {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/projects/%s/merge_requests/%d", f.client.API(), url.PathEscape(path), number)
	answer, err := f.client.Send(ctx, xhttp.JSONParams{
		Method: http.MethodPut,
		URL:    endpoint + "/merge",
	}, nil)
	if errors.Is(err, forge.ErrThrottled) {
		return err
	}

	switch answer.Status {
	case http.StatusNotFound:
		return noMergeRequest(number, path)
	case http.StatusForbidden:
		return forge.ErrUnauthorized.Saying("gitlab will not let the credential merge %s: %v", address, err)
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusConflict, http.StatusUnprocessableEntity:
		return forge.ErrNotMergeable.Saying("gitlab will not merge !%d: %s", number, f.mergeRefusal(ctx, endpoint, graphql.MessageOf(answer.Body)))
	}

	return err
}

// SetDraft marks the merge request a draft or ready, asking nothing of GitLab when it already
// stands that way.
func (f *Forge) SetDraft(ctx context.Context, address string, draft bool) error {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return err
	}

	target, err := f.Get(ctx, address)
	if err != nil {
		return err
	}
	switch {
	case target.MergedAt != nil:
		return forge.ErrNotDraftable.Saying("gitlab will not change !%d: it has already merged", number)
	case target.ClosedAt != nil:
		return forge.ErrNotDraftable.Saying("gitlab will not change !%d: it was closed", number)
	case target.Draft == draft:
		return nil
	}

	var data struct {
		MergeRequestSetDraft *struct {
			Errors []string `json:"errors"`
		} `json:"mergeRequestSetDraft"`
	}
	answer, err := f.client.Query(ctx, setDraftMutation, map[string]any{
		"path":  path,
		"iid":   strconv.Itoa(number),
		"draft": draft,
	}, &data)
	if err != nil {
		return err
	}
	if data.MergeRequestSetDraft == nil {
		if err := failureOf(answer); err != nil {
			return err
		}

		return fmt.Errorf("%w: gitlab has no merge request %d in %s", forge.ErrNotFound, number, path)
	}
	if said := data.MergeRequestSetDraft.Errors; len(said) > 0 {
		return forge.ErrNotDraftable.Saying("gitlab will not change !%d: %s", number, strings.Join(said, "; "))
	}

	return failureOf(answer)
}

// SetAutoMerge asks GitLab to merge the merge request once its pipeline passes, held to the head
// the person saw, or cancels that, refusing one GitLab offers no strategy for since GitLab would
// merge it at once.
func (f *Forge) SetAutoMerge(ctx context.Context, params forge.SetAutoMergeParams) error {
	path, number, err := parseMergeRequestURL(params.URL)
	if err != nil {
		return err
	}

	target, err := f.readAutoMergeTarget(ctx, path, number)
	if err != nil {
		return err
	}
	switch {
	case target.State == "merged":
		return forge.ErrNoAutoMerge.Saying("gitlab will not change !%d: it has already merged", number)
	case target.State == "closed":
		return forge.ErrNoAutoMerge.Saying("gitlab will not change !%d: it was closed", number)
	case target.AutoMergeEnabled == params.On:
		return nil
	case params.On && len(target.AvailableAutoMergeStrategies) == 0:
		return forge.ErrNoAutoMerge.Saying("gitlab will not queue !%d: it has no running pipeline to wait for, or the credential cannot merge it", number)
	}

	return f.sendAutoMerge(ctx, sendAutoMergeParams{
		path:   path,
		number: number,
		change: params,
	})
}

// readAutoMergeTarget reads the merge request whose auto-merge is about to change.
func (f *Forge) readAutoMergeTarget(ctx context.Context, path string, number int) (*autoMergeTarget, error) {
	var data struct {
		Project *struct {
			MergeRequest *autoMergeTarget `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, autoMergeTargetQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return nil, noMergeRequest(number, path)
	}

	return data.Project.MergeRequest, nil
}

// sendAutoMergeParams names the merge request and the change asked of its auto-merge.
type sendAutoMergeParams struct {
	path   string
	number int
	change forge.SetAutoMergeParams
}

// sendAutoMerge queues the merge request to merge once its pipeline passes, or cancels that,
// over GitLab's REST API.
func (f *Forge) sendAutoMerge(ctx context.Context, params sendAutoMergeParams) error {
	endpoint := fmt.Sprintf("%s/projects/%s/merge_requests/%d", f.client.API(), url.PathEscape(params.path), params.number)
	request := xhttp.JSONParams{
		Method: http.MethodPost,
		URL:    endpoint + "/cancel_merge_when_pipeline_succeeds",
	}
	if params.change.On {
		body := map[string]any{
			"auto_merge":                   true,
			"merge_when_pipeline_succeeds": true,
		}
		if params.change.HeadSHA != "" {
			body["sha"] = params.change.HeadSHA
		}
		request = xhttp.JSONParams{
			Method: http.MethodPut,
			URL:    endpoint + "/merge",
			Body:   body,
		}
	}

	sent, err := f.client.Send(ctx, request, nil)
	if errors.Is(err, forge.ErrThrottled) {
		return err
	}

	switch sent.Status {
	case http.StatusNotFound:
		return noMergeRequest(params.number, params.path)
	case http.StatusForbidden:
		return forge.ErrUnauthorized.Saying("gitlab will not let the credential change auto-merge on %s: %v", params.change.URL, err)
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusConflict, http.StatusUnprocessableEntity:
		return forge.ErrNoAutoMerge.Saying("gitlab will not change auto-merge on !%d: %s", params.number, graphql.MessageOf(sent.Body))
	}

	return err
}

// mergeRefusal is why GitLab says a merge request cannot merge, in a person's words.
func (f *Forge) mergeRefusal(ctx context.Context, endpoint string, said string) string {
	var standing struct {
		State               string `json:"state"`
		Draft               bool   `json:"draft"`
		DetailedMergeStatus string `json:"detailed_merge_status"`
	}
	if _, err := f.client.Send(ctx, xhttp.JSONParams{Method: http.MethodGet, URL: endpoint}, &standing); err != nil {
		return said
	}

	switch standing.State {
	case "merged":
		return "it has already merged"
	case "closed":
		return "it was closed without merging"
	case "locked":
		return "it is already being merged"
	}
	if standing.Draft {
		return "it is still a draft"
	}

	switch standing.DetailedMergeStatus {
	case "unchecked", "checking", "approvals_syncing", "preparing":
		return "gitlab is still checking whether it can merge, so try again in a moment"
	case "conflict":
		return "it has merge conflicts"
	case "draft_status":
		return "it is still a draft"
	case "need_rebase":
		return "it has to be rebased onto its target first"
	case "ci_must_pass", "ci_still_running":
		return "its pipeline has to pass first"
	case "not_approved", "requested_changes":
		return "it has not been approved"
	case "discussions_not_resolved":
		return "its threads are not all resolved"
	case "", "mergeable":
		return said
	}

	return "its merge status is " + strings.ReplaceAll(standing.DetailedMergeStatus, "_", " ")
}

// ReadBranches reads the newest merge requests opened from each branch, with their standing,
// aliased into as few queries as GitLab's complexity limit allows, keeping only those whose
// source is the project itself.
func (f *Forge) ReadBranches(ctx context.Context, repo git.RemoteURL, branches []string) (*forge.Reading, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}

	reading := &forge.Reading{}
	err = sendInBatches(ctx, branches, branchesPerQuery, func(ctx context.Context, batch []string) (complexity, error) {
		variables := map[string]any{"path": path}
		var declared, fields strings.Builder
		for i, branch := range batch {
			variables["b"+strconv.Itoa(i)] = []string{branch}
			fmt.Fprintf(&declared, ", $b%d: [String!]", i)
			fmt.Fprintf(&fields, "\n    b%d: mergeRequests(sourceBranches: $b%d, sort: CREATED_DESC, first: %d) { nodes { ...mergeRequestFields %s } }", i, i, mergeRequestsPerBranch, standingFields)
		}
		operation := "query ReadBranches($path: ID!" + declared.String() + ") {\n  project(fullPath: $path) {" + fields.String() + "\n  }\n  queryComplexity { score limit }\n}\n" + mergeRequestFragment

		var data struct {
			Project map[string]*struct {
				Nodes []mergeRequestNode `json:"nodes"`
			} `json:"project"`
			QueryComplexity complexity `json:"queryComplexity"`
		}
		answer, err := f.client.Query(ctx, operation, variables, &data)
		if err != nil {
			return complexity{}, err
		}
		if err := failureOf(answer); err != nil {
			return complexity{}, err
		}
		if data.Project == nil {
			return complexity{}, fmt.Errorf("%w: gitlab has no project %s, which is also its answer for one the credential cannot see", forge.ErrNotFound, path)
		}

		reading.RateLimit = answer.RateLimit
		for i := range batch {
			answered := data.Project["b"+strconv.Itoa(i)]
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

		return data.QueryComplexity, nil
	})
	if err != nil {
		return nil, err
	}

	return reading, nil
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

	return graphql.PageOf(&data.Project.MergeRequests, answer.RateLimit, func(node mergeRequestNode) forge.PullRequest {
		return node.toPullRequest(false)
	}), nil
}

// sendInBatches sends one query per batch of branches, sized from what GitLab last said about its
// complexity budget, halved when it refuses a batch for exceeding it and never again as large.
func sendInBatches(ctx context.Context, branches []string, size int, send func(context.Context, []string) (complexity, error)) error {
	ceiling := len(branches)
	for len(branches) > 0 {
		count := min(size, ceiling, len(branches))
		measured, err := send(ctx, branches[:count])
		if errors.Is(err, errTooComplex) && count > 1 {
			ceiling = count - 1
			size = count / 2

			continue
		}
		if err != nil {
			return err
		}

		branches = branches[count:]
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

// noMergeRequest says GitLab has no such merge request, which is also its answer for a project the credential cannot see.
func noMergeRequest(number int, path string) error {
	return fmt.Errorf("%w: gitlab has no merge request %d in %s, which is also its answer for a project the credential cannot see", forge.ErrNotFound, number, path)
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

// autoMergeTarget is a merge request whose auto-merge is about to change.
type autoMergeTarget struct {
	State                        string   `json:"state"`
	AutoMergeEnabled             bool     `json:"autoMergeEnabled"`
	AvailableAutoMergeStrategies []string `json:"availableAutoMergeStrategies"`
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
	DetailedMergeStatus          string   `json:"detailedMergeStatus"`
	AutoMergeEnabled             bool     `json:"autoMergeEnabled"`
	AvailableAutoMergeStrategies []string `json:"availableAutoMergeStrategies"`
}

// autoMerge is whether the merge request is set to merge on its own, and whether GitLab offers a
// way to set it, which it does only while there is a pipeline to wait for and the credential
// may merge.
func (n standingNode) autoMerge() forge.AutoMerge {
	return forge.AutoMerge{
		On:      n.AutoMergeEnabled,
		Allowed: len(n.AvailableAutoMergeStrategies) > 0,
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
		pullRequest.AutoMerge = xptr.Ptr(n.autoMerge())
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

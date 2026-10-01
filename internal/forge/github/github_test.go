package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures carry the shape GitHub answered on 20 September 2026, with the rows from other
// repositories made up: komrad-forge-fixture#1 open, komrad#742 merged, widgets#55 from a fork,
// widgets#13 from a fork since deleted, widgets#81544 a draft, and the standing fields GitHub
// answers beside each on a read of branches.

const openPull = `{"id":"PR_kwDOUgkrbM8AAAABEIdmHw","number":1,"title":"fixture: open","url":"https://github.com/harrisoncramer/komrad-forge-fixture/pull/1","state":"OPEN","isDraft":false,"createdAt":"2026-09-18T19:14:17Z","updatedAt":"2026-09-18T19:14:17Z","mergedAt":null,"closedAt":null,"headRefName":"fixture/open","headRefOid":"acc6d4fd6caf3511c998a673406e6fed2df7a28b","baseRefName":"main","baseRefOid":"8b32d7985e62febbf494e815545c22b446276b5d","headRepository":{"url":"https://github.com/harrisoncramer/komrad-forge-fixture"},"baseRepository":{"url":"https://github.com/harrisoncramer/komrad-forge-fixture"}}`

const mergedPull = `{"id":"PR_kwDOTgPfPc8AAAABEU5L_Q","number":742,"title":"feat(pullrequest): draw pull request marks in the panel","url":"https://github.com/harrisoncramer/komrad/pull/742","state":"MERGED","isDraft":false,"createdAt":"2026-09-20T17:57:28Z","updatedAt":"2026-09-20T18:00:07Z","mergedAt":"2026-09-20T18:00:07Z","closedAt":"2026-09-20T18:00:07Z","headRefName":"agent/01a0bea9-02d2-7ce0-bbb4-1fbfc3e74198","headRefOid":"70b1d987cdd21940203ca3fbc5b3a7998a47c830","baseRefName":"main","baseRefOid":"79a1ad8fe89cca4494a40c5c5015f46458ddc121","headRepository":{"url":"https://github.com/harrisoncramer/komrad"},"baseRepository":{"url":"https://github.com/harrisoncramer/komrad"}}`

const forkPull = `{"id":"MDExOlB1bGxSZXF1ZXN0MDAwMDAwMDE=","number":55,"title":"Typo correction","url":"https://github.com/acme/widgets/pull/55","state":"CLOSED","isDraft":false,"createdAt":"2014-06-11T05:07:11Z","updatedAt":"2014-06-13T12:41:36Z","mergedAt":null,"closedAt":"2014-06-11T05:07:37Z","headRefName":"fix-typo","headRefOid":"7a97b155f47ac6fc34eaa630d96ba9caf244d80c","baseRefName":"master","baseRefOid":"fd5d98f21b5008f27513d52ac2faa78292a8a3a8","headRepository":{"url":"https://github.com/someone/widgets"},"baseRepository":{"url":"https://github.com/acme/widgets"}}`

const deletedForkPull = `{"id":"MDExOlB1bGxSZXF1ZXN0MDAwMDAwMDI=","number":13,"title":"client: add the service tests","url":"https://github.com/acme/widgets/pull/13","state":"CLOSED","isDraft":false,"createdAt":"2014-06-07T11:41:26Z","updatedAt":"2014-06-20T06:20:42Z","mergedAt":null,"closedAt":"2014-06-20T06:20:42Z","headRefName":"client-tests","headRefOid":"2c8de7699fd79407246011a82201718588a06a08","baseRefName":"master","baseRefOid":"ba7fe835ce990977997bfc6b6c914d235d9ae72f","headRepository":null,"baseRepository":{"url":"https://github.com/acme/widgets"}}`

const draftPull = `{"id":"PR_kwDOAAAAAAAAAAABAAAAAA","number":81544,"title":"unicode: widen the pair members","url":"https://github.com/acme/widgets/pull/81544","state":"OPEN","isDraft":true,"createdAt":"2026-09-15T17:53:12Z","updatedAt":"2026-09-15T18:47:47Z","mergedAt":null,"closedAt":null,"headRefName":"widen-pairs","headRefOid":"695f59abb90db534d1ce005c6b2fc1779728d2b1","baseRefName":"master","baseRefOid":"d90cb4e010dbbf7f39b993db5e64e2904c74e4a4","headRepository":{"url":"https://github.com/someone/widgets"},"baseRepository":{"url":"https://github.com/acme/widgets"}}`

type fakeAuth struct {
	token       string
	invalidated int
}

func (f *fakeAuth) Credential(context.Context) (forge.Credential, error) {
	return forge.Credential{Header: forge.HeaderBearer, Token: forge.NewSecret(f.token)}, nil
}

func (f *fakeAuth) Invalidate(context.Context) error {
	f.invalidated++

	return nil
}

// call is one request the fake GitHub saw.
type call struct {
	Path      string
	Operation string
	Query     string
	Variables map[string]any
	Header    http.Header
}

// answer is what the fake GitHub says to one operation or REST path.
type answer struct {
	status int
	header map[string]string
	body   string
}

// fakeGitHub answers each named GraphQL operation, and each REST path, with one body, and
// records every request it saw.
type fakeGitHub struct {
	answers map[string]answer
	calls   []call
}

func (g *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seen := call{Path: r.URL.Path, Header: r.Header}
	key := r.URL.Path
	if r.URL.Path == "/graphql" {
		var posted struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&posted)
		seen.Operation = graphql.OperationOf(posted.Query)
		seen.Query = posted.Query
		seen.Variables = posted.Variables
		key = seen.Operation
	}
	g.calls = append(g.calls, seen)

	reply, ok := g.answers[key]
	if !ok {
		http.NotFound(w, r)

		return
	}
	for name, value := range reply.header {
		w.Header().Set(name, value)
	}
	if reply.status != 0 {
		w.WriteHeader(reply.status)
	}
	_, _ = w.Write([]byte(reply.body))
}

// last is the most recent request the fake saw.
func (g *fakeGitHub) last(t *testing.T) call {
	t.Helper()
	require.NotEmpty(t, g.calls)

	return g.calls[len(g.calls)-1]
}

// serve is a GitHub answering each operation with one body, recording the requests it saw.
func serve(t *testing.T, answers map[string]answer) (*Forge, *fakeAuth, *fakeGitHub) {
	t.Helper()

	fake := &fakeGitHub{answers: answers}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	auth := &fakeAuth{token: "secret"}

	return NewForge(NewForgeParams{
		Auth: auth,
		API:  server.URL,
		Next: server.Client().Transport,
		Now: func() time.Time {
			return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
		},
	}), auth, fake
}

// data wraps nodes into the data envelope GitHub answers.
func data(body string) string {
	return `{"data":` + body + `}`
}

// listing is a page of a repository's pullRequests connection holding the nodes.
func listing(hasNextPage bool, cursor string, nodes ...string) string {
	page := `{"pageInfo":{"hasNextPage":` + boolOf(hasNextPage) + `,"endCursor":"` + cursor + `"},"nodes":[` + strings.Join(nodes, ",") + `]}`

	return data(`{"repository":{"pullRequests":` + page + `}}`)
}

func boolOf(value bool) string {
	if value {
		return "true"
	}

	return "false"
}

var komrad = git.RemoteURL{
	Raw:    "git@github.com:harrisoncramer/komrad.git",
	Scheme: "ssh",
	Host:   "github.com",
	Path:   "harrisoncramer/komrad",
}

var fixture = git.RemoteURL{
	Raw:    "https://github.com/harrisoncramer/komrad-forge-fixture",
	Scheme: "https",
	Host:   "github.com",
	Path:   "harrisoncramer/komrad-forge-fixture",
}

// TestList covers the branch listing: the variables GitHub wants, the token on the request,
// and every row mapped into the neutral record, a fork and a deleted fork included.
func TestList(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"List": {body: listing(false, "end", openPull, mergedPull, forkPull, deletedForkPull, draftPull)},
	})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    fixture,
		Branch:  "fixture/open",
		Cursor:  "c2",
		PerPage: 50,
	})
	require.NoError(t, err)

	seen := fake.last(t)
	assert.Equal(t, "/graphql", seen.Path)
	assert.Equal(t, "List", seen.Operation)
	assert.Equal(t, listQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"owner":  "harrisoncramer",
		"name":   "komrad-forge-fixture",
		"first":  float64(50),
		"after":  "c2",
		"branch": "fixture/open",
		"states": []any{"OPEN", "CLOSED", "MERGED"},
	}, seen.Variables)
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))
	assert.False(t, listed.HasMore)
	assert.Equal(t, "end", listed.Cursor)
	pullRequests := listed.PullRequests
	require.Len(t, pullRequests, 5)

	repository := git.RemoteURL{
		Raw:    "https://github.com/harrisoncramer/komrad-forge-fixture",
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/komrad-forge-fixture",
	}
	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_kwDOUgkrbM8AAAABEIdmHw",
		URL:        "https://github.com/harrisoncramer/komrad-forge-fixture/pull/1",
		Repository: repository,
		Head:       repository,
		Number:     1,
		Title:      "fixture: open",
		HeadBranch: "fixture/open",
		HeadSHA:    "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
		BaseBranch: "main",
		BaseSHA:    "8b32d7985e62febbf494e815545c22b446276b5d",
		State:      "OPEN",
		CreatedAt:  time.Date(2026, 9, 18, 19, 14, 17, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 18, 19, 14, 17, 0, time.UTC),
	}, pullRequests[0])
	assert.False(t, pullRequests[0].IsFinished())
	assert.Nil(t, pullRequests[0].Decision, "a listing does not ask for the decision")

	stamp := time.Date(2026, 9, 20, 18, 0, 7, 0, time.UTC)
	assert.Equal(t, "PR_kwDOTgPfPc8AAAABEU5L_Q", pullRequests[1].ProviderID)
	assert.Equal(t, "MERGED", pullRequests[1].State)
	assert.Equal(t, &stamp, pullRequests[1].MergedAt)
	assert.Equal(t, &stamp, pullRequests[1].ClosedAt)
	assert.True(t, pullRequests[1].IsFinished())

	assert.Equal(t, "someone/widgets", pullRequests[2].Head.Path)

	assert.True(t, pullRequests[3].Head.IsZero())
	assert.True(t, pullRequests[3].IsFinished())

	assert.True(t, pullRequests[4].Draft)
}

// TestListWithoutABranch covers the repository's own listing: open ones only, no branch.
func TestListWithoutABranch(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"List": {body: listing(false, "", openPull)}})

	listed, err := f.List(t.Context(), forge.ListParams{Repo: fixture})
	require.NoError(t, err)
	require.Len(t, listed.PullRequests, 1)
	seen := fake.last(t)
	assert.Equal(t, []any{"OPEN"}, seen.Variables["states"])
	assert.Nil(t, seen.Variables["after"], "the first page names no cursor")
	assert.NotContains(t, seen.Variables, "branch")
	assert.InDelta(t, 1, seen.Variables["first"], 0)
}

// TestListReadsTheNextPage covers GitHub saying another page follows, and where it starts.
func TestListReadsTheNextPage(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, map[string]answer{"List": {body: listing(true, "Y3Vyc29yOnYyOpK0", openPull)}})

	listed, err := f.List(t.Context(), forge.ListParams{Repo: fixture})
	require.NoError(t, err)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "Y3Vyc29yOnYyOpK0", listed.Cursor)
}

// failingStanding and passingStanding are the standing fields GitHub answers beside a pull
// request on a read of branches, the recorded shape with the values made up.
const failingStanding = `"reviewDecision":"CHANGES_REQUESTED","mergeStateStatus":"UNSTABLE","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]},"autoMergeRequest":null,"viewerCanEnableAutoMerge":true`

const passingStanding = `"reviewDecision":"APPROVED","mergeStateStatus":"CLEAN","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]},"autoMergeRequest":{"enabledAt":"2026-09-20T11:00:00Z"},"viewerCanEnableAutoMerge":false`

const closedFixturePull = `{"id":"PR_kwDOUgkrbM8AAAABEIdmIA","number":2,"title":"fixture: closed","url":"https://github.com/harrisoncramer/komrad-forge-fixture/pull/2","state":"CLOSED","isDraft":false,"createdAt":"2026-09-17T10:00:00Z","updatedAt":"2026-09-17T11:00:00Z","mergedAt":null,"closedAt":"2026-09-17T11:00:00Z","headRefName":"fixture/closed","headRefOid":"5555555555555555555555555555555555555555","baseRefName":"main","baseRefOid":"8b32d7985e62febbf494e815545c22b446276b5d","headRepository":{"url":"https://github.com/harrisoncramer/komrad-forge-fixture"},"baseRepository":{"url":"https://github.com/harrisoncramer/komrad-forge-fixture"}}`

// withStanding is a pull request node carrying the standing fields.
func withStanding(node, standing string) string {
	return strings.TrimSuffix(node, "}") + "," + standing + "}"
}

// branchesRead is GitHub answering a read of branches, one alias per branch.
func branchesRead(aliases ...string) string {
	fields := make([]string, len(aliases))
	for i, nodes := range aliases {
		fields[i] = `"b` + strconv.Itoa(i) + `":{"nodes":[` + nodes + `]}`
	}

	return data(`{"repository":{` + strings.Join(fields, ",") + `}}`)
}

// TestReadBranches covers the aliased query, each answer carrying its standing, the rate limit
// off the headers, and only pull requests from the repository itself kept.
func TestReadBranches(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ReadBranches": {
		header: map[string]string{"X-RateLimit-Remaining": "4321", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1789900000"},
		body: branchesRead(
			withStanding(openPull, failingStanding)+","+withStanding(forkPull, failingStanding),
			withStanding(deletedForkPull, passingStanding),
			withStanding(closedFixturePull, passingStanding),
		),
	}})

	reading, err := f.ReadBranches(t.Context(), fixture, []string{"fixture/open", "client-tests", "fixture/closed"})
	require.NoError(t, err)

	require.Len(t, fake.calls, 1)
	seen := fake.last(t)
	assert.Equal(t, "ReadBranches", seen.Operation)
	assert.Equal(t, map[string]any{
		"owner": "harrisoncramer",
		"name":  "komrad-forge-fixture",
		"b0":    "fixture/open",
		"b1":    "client-tests",
		"b2":    "fixture/closed",
	}, seen.Variables)
	assert.Contains(t, seen.Query, "query ReadBranches($owner: String!, $name: String!, $b0: String!, $b1: String!, $b2: String!)")
	assert.Contains(t, seen.Query, "b0: pullRequests(headRefName: $b0, first: 5, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { ...pullRequestFields "+standingFields+" } }")
	assert.Contains(t, seen.Query, pullRequestFragment)

	assert.Equal(t, forge.RateLimit{Known: true, Remaining: 4321, Limit: 5000, ResetAt: time.Unix(1789900000, 0)}, reading.RateLimit)

	require.Len(t, reading.PullRequests, 2, "the fork and the deleted fork are left out")
	open, closed := reading.PullRequests[0], reading.PullRequests[1]
	assert.Equal(t, 1, open.Number)
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionChangesRequested), open.Decision)
	assert.Equal(t, xptr.Ptr(forge.ChecksFailing), open.Checks)
	assert.Equal(t, xptr.Ptr(forge.MergeabilityUnstable), open.Mergeability)
	assert.Equal(t, &forge.AutoMerge{Allowed: true}, open.AutoMerge)
	assert.Equal(t, 2, closed.Number)
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionApproved), closed.Decision)
	assert.Equal(t, xptr.Ptr(forge.ChecksPassing), closed.Checks)
	assert.Equal(t, xptr.Ptr(forge.MergeabilityClean), closed.Mergeability)
	assert.Equal(t, &forge.AutoMerge{On: true}, closed.AutoMerge)
}

// TestReadBranchesBatches covers more branches than one query takes going out as a second query.
func TestReadBranchesBatches(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ReadBranches": {body: branchesRead(withStanding(openPull, failingStanding))}})

	branches := make([]string, branchesPerQuery+1)
	for i := range branches {
		branches[i] = "branch-" + strconv.Itoa(i)
	}
	reading, err := f.ReadBranches(t.Context(), fixture, branches)
	require.NoError(t, err)

	require.Len(t, fake.calls, 2)
	assert.Len(t, fake.calls[0].Variables, branchesPerQuery+2)
	assert.Equal(t, "branch-49", fake.calls[0].Variables["b49"])
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "komrad-forge-fixture", "b0": "branch-50"}, fake.calls[1].Variables)
	assert.Len(t, reading.PullRequests, 2)
}

// TestReadBranchesRefusals covers a repository GitHub does not have and a path it cannot
// address, the last costing no request.
func TestReadBranchesRefusals(t *testing.T) {
	t.Parallel()

	missing, _, _ := serve(t, map[string]answer{"ReadBranches": {body: `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],"locations":[{"line":2,"column":3}],"message":"Could not resolve to a Repository with the name 'harrisoncramer/gone'."}]}`}})
	_, err := missing.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "harrisoncramer/gone"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotFound)

	f, _, fake := serve(t, nil)
	_, err = f.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

// TestGet covers reading one pull request by its web address.
func TestGet(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Get": {body: data(`{"repository":{"pullRequest":` + mergedPull + `}}`)}})

	pullRequest, err := f.Get(t.Context(), "https://github.com/harrisoncramer/komrad/pull/742")
	require.NoError(t, err)
	assert.Equal(t, 742, pullRequest.Number)
	assert.Equal(t, "feat(pullrequest): draw pull request marks in the panel", pullRequest.Title)
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "komrad", "number": float64(742)}, fake.last(t).Variables)
}

// TestMerge covers merging with the repository's default method and GitHub's refusals.
func TestMerge(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	target := answer{body: data(`{"repository":{"viewerDefaultMergeMethod":"SQUASH","pullRequest":{"id":"PR_742","state":"OPEN"}}}`)}

	tests := []struct {
		name    string
		answers map[string]answer
		wantErr error
	}{
		{
			name: "the pull request merges with the method the repository defaults to",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       {body: data(`{"mergePullRequest":{"clientMutationId":null}}`)},
			},
		},
		{
			name: "a pull request github will not merge",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       {body: `{"data":{"mergePullRequest":null},"errors":[{"type":"UNPROCESSABLE","path":["mergePullRequest"],"message":"Pull Request is not mergeable"}]}`},
			},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a credential github will not let merge",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       {body: `{"data":{"mergePullRequest":null},"errors":[{"type":"FORBIDDEN","path":["mergePullRequest"],"message":"Resource not accessible by integration"}]}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:    "a pull request that has already merged is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{"repository":{"viewerDefaultMergeMethod":"MERGE","pullRequest":{"id":"PR_742","state":"MERGED"}}}`)}},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name:    "a closed pull request is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{"repository":{"viewerDefaultMergeMethod":"MERGE","pullRequest":{"id":"PR_742","state":"CLOSED"}}}`)}},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a pull request github does not have",
			answers: map[string]answer{
				"MergeTarget": {body: data(`{"repository":{"viewerDefaultMergeMethod":"MERGE","pullRequest":null}}`)},
			},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.Merge(t.Context(), address)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"id": "PR_742", "method": "SQUASH"}, fake.last(t).Variables)
		})
	}
}

// TestSetDraft covers converting a pull request each way and the forge's refusals.
func TestSetDraft(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	open := answer{body: data(`{"repository":{"pullRequest":{"id":"PR_742","state":"OPEN","isDraft":false}}}`)}
	drafted := answer{body: data(`{"repository":{"pullRequest":{"id":"PR_742","state":"OPEN","isDraft":true}}}`)}

	tests := []struct {
		name          string
		draft         bool
		answers       map[string]answer
		wantErr       error
		wantOperation string
	}{
		{
			name:          "an open pull request is converted to a draft",
			draft:         true,
			answers:       map[string]answer{"DraftTarget": open, "ConvertToDraft": {body: data(`{"convertPullRequestToDraft":{"clientMutationId":null}}`)}},
			wantOperation: "ConvertToDraft",
		},
		{
			name:          "a draft is marked ready for review",
			answers:       map[string]answer{"DraftTarget": drafted, "MarkReady": {body: data(`{"markPullRequestReadyForReview":{"clientMutationId":null}}`)}},
			wantOperation: "MarkReady",
		},
		{
			name:          "a draft asked to be a draft is left alone",
			draft:         true,
			answers:       map[string]answer{"DraftTarget": drafted},
			wantOperation: "DraftTarget",
		},
		{
			name:    "a merged pull request is refused before github is asked to change it",
			draft:   true,
			answers: map[string]answer{"DraftTarget": {body: data(`{"repository":{"pullRequest":{"id":"PR_742","state":"MERGED","isDraft":false}}}`)}},
			wantErr: forge.ErrNotDraftable,
		},
		{
			name:  "a pull request github will not convert",
			draft: true,
			answers: map[string]answer{
				"DraftTarget":    open,
				"ConvertToDraft": {body: `{"data":{"convertPullRequestToDraft":null},"errors":[{"type":"UNPROCESSABLE","path":["convertPullRequestToDraft"],"message":"Draft pull requests are not supported in this repository"}]}`},
			},
			wantErr: forge.ErrNotDraftable,
		},
		{
			name:  "a credential github will not let change the pull request",
			draft: true,
			answers: map[string]answer{
				"DraftTarget":    open,
				"ConvertToDraft": {body: `{"data":{"convertPullRequestToDraft":null},"errors":[{"type":"FORBIDDEN","path":["convertPullRequestToDraft"],"message":"Resource not accessible by integration"}]}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:    "a pull request github does not have",
			answers: map[string]answer{"DraftTarget": {body: data(`{"repository":{"pullRequest":null}}`)}},
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetDraft(t.Context(), address, tt.draft)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOperation, fake.last(t).Operation)
		})
	}
}

// TestSetAutoMerge covers queueing a pull request held to the head the person saw, withdrawing
// it, leaving one alone that already stands that way, and refusing a finished one before GitHub
// is asked to change it.
func TestSetAutoMerge(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	target := func(state, request string) answer {
		return answer{body: data(`{"repository":{"viewerDefaultMergeMethod":"SQUASH","pullRequest":{"id":"PR_742","state":"` + state + `","autoMergeRequest":` + request + `}}}`)}
	}
	queued := `{"enabledAt":"2026-09-20T12:00:00Z"}`

	tests := []struct {
		name          string
		params        forge.SetAutoMergeParams
		answers       map[string]answer
		wantErr       error
		wantOperation string
		wantVariables map[string]any
	}{
		{
			name:    "a pull request github does not have",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": {body: data(`{"repository":{"pullRequest":null}}`)}},
			wantErr: forge.ErrNotFound,
		},
		{
			name:    "a merged pull request is refused before github is asked to change it",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": target("MERGED", "null")},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:   "a pull request github will not queue",
			params: forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{
				"AutoMergeTarget": target("OPEN", "null"),
				"EnableAutoMerge": {body: `{"data":{"enablePullRequestAutoMerge":null},"errors":[{"type":"UNPROCESSABLE","path":["enablePullRequestAutoMerge"],"message":"Pull request is in clean status"}]}`},
			},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:          "a queued pull request asked to be queued is left alone",
			params:        forge.SetAutoMergeParams{URL: address, On: true},
			answers:       map[string]answer{"AutoMergeTarget": target("OPEN", queued)},
			wantOperation: "AutoMergeTarget",
			wantVariables: map[string]any{"owner": "harrisoncramer", "name": "komrad", "number": float64(742)},
		},
		{
			name: "an open pull request is queued with the default method at the head the person saw",
			params: forge.SetAutoMergeParams{
				URL:     address,
				HeadSHA: "abc123",
				On:      true,
			},
			answers: map[string]answer{
				"AutoMergeTarget": target("OPEN", "null"),
				"EnableAutoMerge": {body: data(`{"enablePullRequestAutoMerge":{"clientMutationId":null}}`)},
			},
			wantOperation: "EnableAutoMerge",
			wantVariables: map[string]any{"id": "PR_742", "method": "SQUASH", "head": "abc123"},
		},
		{
			name:   "a queued pull request is withdrawn",
			params: forge.SetAutoMergeParams{URL: address},
			answers: map[string]answer{
				"AutoMergeTarget":  target("OPEN", queued),
				"DisableAutoMerge": {body: data(`{"disablePullRequestAutoMerge":{"clientMutationId":null}}`)},
			},
			wantOperation: "DisableAutoMerge",
			wantVariables: map[string]any{"id": "PR_742"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetAutoMerge(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			seen := fake.last(t)
			assert.Equal(t, tt.wantOperation, seen.Operation)
			assert.Equal(t, tt.wantVariables, seen.Variables)
		})
	}
}

// TestGetRefusesWhatIsNotAPullRequest covers the addresses Get will not ask GitHub about.
func TestGetRefusesWhatIsNotAPullRequest(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	tests := []struct {
		name    string
		address string
	}{
		{name: "a merge request path", address: "https://gitlab.com/owner/repo/-/merge_requests/1"},
		{name: "an issue", address: "https://github.com/owner/repo/issues/1"},
		{name: "no number", address: "https://github.com/owner/repo/pull/"},
		{name: "a repository", address: "https://github.com/owner/repo"},
		{name: "not a url", address: "::not a url::"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := f.Get(t.Context(), tt.address)
			require.ErrorIs(t, err, forge.ErrNotPullRequest)
			assert.Empty(t, fake.calls)
		})
	}
}

// TestListRefusesANestedPath covers a repository path GitHub cannot address.
func TestListRefusesANestedPath(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	_, err = f.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

// TestUnauthorizedInvalidatesTheCredential covers a refused token: GitHub's 401 reaches the
// Authenticator so the connection reads as unverified.
func TestUnauthorizedInvalidatesTheCredential(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, map[string]answer{"List": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})

	_, err := f.List(t.Context(), forge.ListParams{Repo: komrad, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestVerify covers asking GitHub who the credential is, and a token GitHub refuses.
func TestVerify(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Verify": {body: `{"data":{"viewer":{"login":"harrisoncramer"}}}`}})

	login, err := f.Verify(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
	seen := fake.last(t)
	assert.Equal(t, verifyQuery, seen.Query)
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))

	refused, auth, _ := serve(t, map[string]answer{"Verify": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})
	_, err = refused.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestNotFound covers what GitHub answers for a repository or pull request it does not have,
// or will not show, and a host whose GraphQL is somewhere else, which is not that.
func TestNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		call func(f *Forge) error
	}{
		{
			name: "a repository github does not have",
			body: `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],"locations":[{"line":2,"column":3}],"message":"Could not resolve to a Repository with the name 'harrisoncramer/no-such-repository-xyz'."}]}`,
			call: func(f *Forge) error {
				_, err := f.Get(t.Context(), "https://github.com/harrisoncramer/no-such-repository-xyz/pull/1")

				return err
			},
		},
		{
			name: "a pull request github does not have",
			body: `{"data":{"repository":{"pullRequest":null}},"errors":[{"type":"NOT_FOUND","path":["repository","pullRequest"],"locations":[{"line":3,"column":5}],"message":"Could not resolve to a PullRequest with the number of 999."}]}`,
			call: func(f *Forge) error {
				_, err := f.Get(t.Context(), "https://github.com/harrisoncramer/komrad-forge-fixture/pull/999")

				return err
			},
		},
		{
			name: "a repository the listing cannot see",
			body: `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],"locations":[{"line":2,"column":3}],"message":"Could not resolve to a Repository with the name 'harrisoncramer/no-such-repository-xyz'."}]}`,
			call: func(f *Forge) error {
				_, err := f.ReadBranches(t.Context(), komrad, []string{"main"})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, auth, _ := serve(t, map[string]answer{"Get": {body: tt.body}, "ReadBranches": {body: tt.body}})
			require.ErrorIs(t, tt.call(f), forge.ErrNotFound)
			assert.Equal(t, 0, auth.invalidated)
		})
	}

	misrouted, _, _ := serve(t, nil)
	_, err := misrouted.Get(t.Context(), "https://github.com/harrisoncramer/komrad/pull/1")
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrNotFound)
	assert.Contains(t, err.Error(), "/graphql")
}

// TestVerifyInstallation covers the one REST call: how many repositories an installation
// token reaches, and a refusal reaching the Authenticator.
func TestVerifyInstallation(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"/installation/repositories": {body: `{"total_count":4,"repositories":[{"full_name":"harrisoncramer/komrad"}]}`}})

	reached, err := f.VerifyInstallation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, reached)
	assert.Equal(t, "/installation/repositories", fake.last(t).Path)

	refused, auth, _ := serve(t, map[string]answer{"/installation/repositories": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})
	_, err = refused.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)

	missing, _, _ := serve(t, nil)
	_, err = missing.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrNotFound)
}

type refusingAuth struct{ err error }

func (r refusingAuth) Credential(context.Context) (forge.Credential, error) {
	return forge.Credential{}, r.err
}

func (refusingAuth) Invalidate(context.Context) error { return nil }

// TestCredentialFailureIsNotAReachFailure covers the Authenticator refusing before a request
// leaves: what it said is the answer, without the wording a dial failure gets.
func TestCredentialFailureIsNotAReachFailure(t *testing.T) {
	t.Parallel()

	refused := forge.ErrUnauthorized.Saying("github refused to mint an installation token: 401 Unauthorized")
	f := NewForge(NewForgeParams{Auth: refusingAuth{err: refused}, API: "http://127.0.0.1:1"})

	_, err := f.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, refused.Error(), err.Error())
}

// TestRateLimitRefusals covers the ways GitHub says to wait, a primary refusal arriving as a
// 200 among them, and the one 403 that is not a rate limit at all.
func TestRateLimitRefusals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		status        int
		header        map[string]string
		body          string
		wantThrottled bool
		wantRetryAt   time.Time
	}{
		{
			name:          "a bare 403 is a permission answer",
			status:        http.StatusForbidden,
			body:          `{"message":"Resource not accessible by integration"}`,
			wantThrottled: false,
		},
		{
			name:          "retry-after says how long",
			status:        http.StatusForbidden,
			header:        map[string]string{"Retry-After": "30"},
			body:          `{"message":"You have exceeded a secondary rate limit"}`,
			wantThrottled: true,
			wantRetryAt:   now.Add(30 * time.Second),
		},
		{
			name:          "a spent primary budget refused on the status says when it resets",
			status:        http.StatusForbidden,
			header:        map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1789900000"},
			body:          `{"message":"API rate limit exceeded"}`,
			wantThrottled: true,
			wantRetryAt:   time.Unix(1789900000, 0),
		},
		{
			name:          "a spent primary budget refused in the errors says when it resets",
			status:        http.StatusOK,
			header:        map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1789900000"},
			body:          `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded for user ID 12345."}]}`,
			wantThrottled: true,
			wantRetryAt:   time.Unix(1789900000, 0),
		},
		{
			name:          "a 429 with nothing else is a refusal with no time",
			status:        http.StatusTooManyRequests,
			body:          `{"message":"slow down"}`,
			wantThrottled: true,
		},
		{
			name:          "the secondary limit body with no header is a refusal with no time",
			status:        http.StatusForbidden,
			body:          `{"message":"You have triggered an abuse detection mechanism"}`,
			wantThrottled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, map[string]answer{"ReadBranches": {
				status: tt.status,
				header: tt.header,
				body:   tt.body,
			}})

			_, err := f.ReadBranches(t.Context(), komrad, []string{"main"})
			require.Error(t, err)

			var throttled *forge.ThrottledError
			if !tt.wantThrottled {
				assert.NotErrorAs(t, err, &throttled, "a permission answer is not a throttle")
				assert.NotErrorIs(t, err, forge.ErrThrottled)

				return
			}

			require.ErrorAs(t, err, &throttled)
			require.ErrorIs(t, err, forge.ErrThrottled)
			assert.True(t, throttled.RetryAt.Equal(tt.wantRetryAt), "retry at %s, want %s", throttled.RetryAt, tt.wantRetryAt)
		})
	}
}

// TestChecksOf covers every rollup state GitHub documents, and a rollup absent from the answer.
func TestChecksOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rollup *rollupNode
		want   forge.Checks
	}{
		{
			name:   "success is passing",
			rollup: &rollupNode{State: "SUCCESS"},
			want:   forge.ChecksPassing,
		},
		{
			name:   "failure is failing",
			rollup: &rollupNode{State: "FAILURE"},
			want:   forge.ChecksFailing,
		},
		{
			name:   "error is failing",
			rollup: &rollupNode{State: "ERROR"},
			want:   forge.ChecksFailing,
		},
		{
			name:   "pending is pending",
			rollup: &rollupNode{State: "PENDING"},
			want:   forge.ChecksPending,
		},
		{
			name:   "expected is pending",
			rollup: &rollupNode{State: "EXPECTED"},
			want:   forge.ChecksPending,
		},
		{
			name:   "no rollup is none",
			rollup: nil,
			want:   forge.ChecksNone,
		},
		{
			name:   "a state komrad does not know is none",
			rollup: &rollupNode{State: "SOMETHING_NEW"},
			want:   forge.ChecksNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, checksOf(tt.rollup))
		})
	}
}

// TestMergeabilityOf covers every merge state GitHub documents.
func TestMergeabilityOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   forge.Mergeability
	}{
		{status: "CLEAN", want: forge.MergeabilityClean},
		{status: "HAS_HOOKS", want: forge.MergeabilityClean},
		{status: "UNSTABLE", want: forge.MergeabilityUnstable},
		{status: "BLOCKED", want: forge.MergeabilityBlocked},
		{status: "DRAFT", want: forge.MergeabilityBlocked},
		{status: "BEHIND", want: forge.MergeabilityBehind},
		{status: "DIRTY", want: forge.MergeabilityConflicting},
		{status: "UNKNOWN", want: forge.MergeabilityUnknown},
		{status: "", want: forge.MergeabilityUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, mergeabilityOf(tt.status))
		})
	}
}

// TestGraphQLURLSitsBesideTheRESTBase covers github.com and an enterprise host, whose GraphQL
// endpoint is not under its v3 REST path.
func TestGraphQLURLSitsBesideTheRESTBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		api  string
		want string
	}{
		{
			name: "github.com",
			api:  "",
			want: "https://api.github.com/graphql",
		},
		{
			name: "an enterprise host",
			api:  "https://ghe.example.com/api/v3",
			want: "https://ghe.example.com/api/graphql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := NewForge(NewForgeParams{Auth: &fakeAuth{}, API: tt.api})
			assert.Equal(t, tt.want, f.client.URL())
		})
	}
}

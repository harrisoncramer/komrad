package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/forge/graphql"
	"weave/internal/git"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures carry the shape GitHub answered on 20 September 2026, with the rows from other
// repositories made up: weave-forge-fixture#1 open, weave#742 merged, widgets#55 from a fork,
// widgets#13 from a fork since deleted, widgets#81544 a draft, a search page over two
// repositories, and a batch of standings with one number GitHub does not have.

const openPull = `{"id":"PR_kwDOUgkrbM8AAAABEIdmHw","number":1,"title":"fixture: open","url":"https://github.com/harrisoncramer/weave-forge-fixture/pull/1","state":"OPEN","isDraft":false,"createdAt":"2026-09-18T19:14:17Z","updatedAt":"2026-09-18T19:14:17Z","mergedAt":null,"closedAt":null,"headRefName":"fixture/open","headRefOid":"acc6d4fd6caf3511c998a673406e6fed2df7a28b","baseRefName":"main","baseRefOid":"8b32d7985e62febbf494e815545c22b446276b5d","headRepository":{"url":"https://github.com/harrisoncramer/weave-forge-fixture"},"baseRepository":{"url":"https://github.com/harrisoncramer/weave-forge-fixture"}}`

const mergedPull = `{"id":"PR_kwDOTgPfPc8AAAABEU5L_Q","number":742,"title":"feat(pullrequest): draw pull request marks in the panel","url":"https://github.com/harrisoncramer/weave/pull/742","state":"MERGED","isDraft":false,"createdAt":"2026-09-20T17:57:28Z","updatedAt":"2026-09-20T18:00:07Z","mergedAt":"2026-09-20T18:00:07Z","closedAt":"2026-09-20T18:00:07Z","headRefName":"agent/01a0bea9-02d2-7ce0-bbb4-1fbfc3e74198","headRefOid":"70b1d987cdd21940203ca3fbc5b3a7998a47c830","baseRefName":"main","baseRefOid":"79a1ad8fe89cca4494a40c5c5015f46458ddc121","headRepository":{"url":"https://github.com/harrisoncramer/weave"},"baseRepository":{"url":"https://github.com/harrisoncramer/weave"}}`

const forkPull = `{"id":"MDExOlB1bGxSZXF1ZXN0MDAwMDAwMDE=","number":55,"title":"Typo correction","url":"https://github.com/acme/widgets/pull/55","state":"CLOSED","isDraft":false,"createdAt":"2014-06-11T05:07:11Z","updatedAt":"2014-06-13T12:41:36Z","mergedAt":null,"closedAt":"2014-06-11T05:07:37Z","headRefName":"fix-typo","headRefOid":"7a97b155f47ac6fc34eaa630d96ba9caf244d80c","baseRefName":"master","baseRefOid":"fd5d98f21b5008f27513d52ac2faa78292a8a3a8","headRepository":{"url":"https://github.com/someone/widgets"},"baseRepository":{"url":"https://github.com/acme/widgets"}}`

const deletedForkPull = `{"id":"MDExOlB1bGxSZXF1ZXN0MDAwMDAwMDI=","number":13,"title":"client: add the service tests","url":"https://github.com/acme/widgets/pull/13","state":"CLOSED","isDraft":false,"createdAt":"2014-06-07T11:41:26Z","updatedAt":"2014-06-20T06:20:42Z","mergedAt":null,"closedAt":"2014-06-20T06:20:42Z","headRefName":"client-tests","headRefOid":"2c8de7699fd79407246011a82201718588a06a08","baseRefName":"master","baseRefOid":"ba7fe835ce990977997bfc6b6c914d235d9ae72f","headRepository":null,"baseRepository":{"url":"https://github.com/acme/widgets"}}`

const draftPull = `{"id":"PR_kwDOAAAAAAAAAAABAAAAAA","number":81544,"title":"unicode: widen the pair members","url":"https://github.com/acme/widgets/pull/81544","state":"OPEN","isDraft":true,"createdAt":"2026-09-15T17:53:12Z","updatedAt":"2026-09-15T18:47:47Z","mergedAt":null,"closedAt":null,"headRefName":"widen-pairs","headRefOid":"695f59abb90db534d1ce005c6b2fc1779728d2b1","baseRefName":"master","baseRefOid":"d90cb4e010dbbf7f39b993db5e64e2904c74e4a4","headRepository":{"url":"https://github.com/someone/widgets"},"baseRepository":{"url":"https://github.com/acme/widgets"}}`

const searchPage = `{"data":{"search":{"issueCount":159,"pageInfo":{"hasNextPage":true,"endCursor":"Y3Vyc29yOjM="},"nodes":[{"id":"PR_kwDOTgPfPc8AAAABEU9Z2Q","number":744,"title":"feat(ui): one row per directory on the worktrees screen","url":"https://github.com/harrisoncramer/weave/pull/744","state":"MERGED","isDraft":false,"reviewDecision":null,"createdAt":"2026-09-20T18:14:43Z","updatedAt":"2026-09-20T18:17:01Z","mergedAt":"2026-09-20T18:17:01Z","closedAt":"2026-09-20T18:17:01Z","headRefName":"agent/01a0bff6-3c6d-7a9e-b497-e76334bd2ab6","headRefOid":"152131741f12434145b7c8df8652140c218ac0bc","baseRefName":"main","baseRefOid":"0710f696842e4640b08afad7ca4c70cccb8312e6","headRepository":{"url":"https://github.com/harrisoncramer/weave"},"baseRepository":{"url":"https://github.com/harrisoncramer/weave"}},{"id":"PR_kwDOTgPfPc8AAAABEU8L2A","number":743,"title":"perf(worktrees): measure a directory in parallel and by its blocks","url":"https://github.com/harrisoncramer/weave/pull/743","state":"MERGED","isDraft":false,"reviewDecision":null,"createdAt":"2026-09-20T18:09:36Z","updatedAt":"2026-09-20T18:12:58Z","mergedAt":"2026-09-20T18:12:58Z","closedAt":"2026-09-20T18:12:58Z","headRefName":"agent/01a0bec8-14fa-7849-a8fa-f31b25f92847","headRefOid":"fa7b9bd985d17396ddbb416d6599efd5b0054e2f","baseRefName":"main","baseRefOid":"3ff2a452603547ebe7d438ea54ecff19550fb6fa","headRepository":{"url":"https://github.com/harrisoncramer/weave"},"baseRepository":{"url":"https://github.com/harrisoncramer/weave"}},{"id":"PR_kwDOTgPfPc8AAAABEU5L_Q","number":742,"title":"feat(pullrequest): draw pull request marks in the panel","url":"https://github.com/harrisoncramer/weave/pull/742","state":"MERGED","isDraft":false,"reviewDecision":null,"createdAt":"2026-09-20T17:57:28Z","updatedAt":"2026-09-20T18:00:07Z","mergedAt":"2026-09-20T18:00:07Z","closedAt":"2026-09-20T18:00:07Z","headRefName":"agent/01a0bea9-02d2-7ce0-bbb4-1fbfc3e74198","headRefOid":"70b1d987cdd21940203ca3fbc5b3a7998a47c830","baseRefName":"main","baseRefOid":"79a1ad8fe89cca4494a40c5c5015f46458ddc121","headRepository":{"url":"https://github.com/harrisoncramer/weave"},"baseRepository":{"url":"https://github.com/harrisoncramer/weave"}}]}}}`

// searchFailing is one search row as GitHub answers the standings fields on it, the recorded
// shape with the row made up.
const searchFailing = `{"data":{"search":{"issueCount":7,"pageInfo":{"hasNextPage":true,"endCursor":"Y3Vyc29yOjE="},"nodes":[{"id":"PR_kwDOTgPfPc8AAAABEU9Z9Z","number":804,"title":"feat(panel): a row for the checks","url":"https://github.com/harrisoncramer/weave/pull/804","state":"OPEN","isDraft":false,"createdAt":"2026-09-17T22:22:25Z","updatedAt":"2026-09-18T21:44:51Z","mergedAt":null,"closedAt":null,"headRefName":"agent/01a0c3e6-8a93-7441-a4c4-32e36761bb27","headRefOid":"4444444444444444444444444444444444444444","baseRefName":"main","baseRefOid":"06cdb987a45779a11be2fc920da62a489c0c1b71","headRepository":{"url":"https://github.com/harrisoncramer/weave"},"baseRepository":{"url":"https://github.com/harrisoncramer/weave"},"reviewDecision":"REVIEW_REQUIRED","mergeStateStatus":"UNSTABLE","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}}]}}}`

const searchApproved = `{"data":{"search":{"issueCount":2,"pageInfo":{"hasNextPage":false,"endCursor":"Y3Vyc29yOjI="},"nodes":[{"id":"PR_kwDOIGmk9s8AAAABESZ58w","number":11158,"title":"Metrics production image update","url":"https://github.com/acme/deploy/pull/11158","state":"MERGED","isDraft":false,"reviewDecision":"APPROVED","createdAt":"2026-09-20T06:24:15Z","updatedAt":"2026-09-20T06:24:26Z","mergedAt":"2026-09-20T06:24:26Z","closedAt":"2026-09-20T06:24:26Z","headRefName":"update-metrics-images","headRefOid":"41fdd966bfbce0757b89b100efa46f8e28d02218","baseRefName":"main","baseRefOid":"94475aebe2d459bf41137bf67d767023dde7baa3","headRepository":{"url":"https://github.com/acme/deploy"},"baseRepository":{"url":"https://github.com/acme/deploy"}},{"id":"PR_kwDOIGmk9s8AAAABEQX0JA","number":11157,"title":"Admin production image update","url":"https://github.com/acme/deploy/pull/11157","state":"MERGED","isDraft":false,"reviewDecision":"APPROVED","createdAt":"2026-09-19T21:14:05Z","updatedAt":"2026-09-19T21:14:18Z","mergedAt":"2026-09-19T21:14:17Z","closedAt":"2026-09-19T21:14:17Z","headRefName":"update-admin-images","headRefOid":"583d259df0d2f141345f659491de89462a0de6cc","baseRefName":"main","baseRefOid":"24235336d21888966f294aea6434aa64a16cb004","headRepository":{"url":"https://github.com/acme/deploy"},"baseRepository":{"url":"https://github.com/acme/deploy"}}]}}}`

// standingsBatch is the shape GitHub answers the aliased standings query with, the recorded
// shape with the rows made up: a failing head, a pending one, an approved one whose checks
// passed, one with no rollup, and a number GitHub does not have.
const standingsBatch = `{"data":{"repository":{"p801":{"headRefOid":"1111111111111111111111111111111111111111","reviewDecision":"CHANGES_REQUESTED","mergeStateStatus":"UNSTABLE","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},"p802":{"headRefOid":"2222222222222222222222222222222222222222","reviewDecision":"REVIEW_REQUIRED","mergeStateStatus":"BLOCKED","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"PENDING"}}}]}},"p803":{"headRefOid":"3333333333333333333333333333333333333333","reviewDecision":"APPROVED","mergeStateStatus":"CLEAN","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}},"p1":{"headRefOid":"c744e226f1248223edd7aa77a7874aba2eec3b16","reviewDecision":null,"mergeStateStatus":"UNKNOWN","commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}},"p9999999":null}},"errors":[{"type":"NOT_FOUND","path":["repository","p9999999"],"locations":[{"line":7,"column":5}],"message":"Could not resolve to a PullRequest with the number of 9999999."}]}`

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

var weave = git.RemoteURL{
	Raw:    "git@github.com:harrisoncramer/weave.git",
	Scheme: "ssh",
	Host:   "github.com",
	Path:   "harrisoncramer/weave",
}

var fixture = git.RemoteURL{
	Raw:    "https://github.com/harrisoncramer/weave-forge-fixture",
	Scheme: "https",
	Host:   "github.com",
	Path:   "harrisoncramer/weave-forge-fixture",
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
		"name":   "weave-forge-fixture",
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
		Raw:    "https://github.com/harrisoncramer/weave-forge-fixture",
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave-forge-fixture",
	}
	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_kwDOUgkrbM8AAAABEIdmHw",
		URL:        "https://github.com/harrisoncramer/weave-forge-fixture/pull/1",
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

// TestListUpdated covers the sweep's request: every state by last change, the largest page,
// the cursor carried in and out, and the rate limit read off the headers.
func TestListUpdated(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ListUpdated": {
		header: map[string]string{"X-RateLimit-Remaining": "4321", "X-RateLimit-Reset": "1789900000"},
		body:   listing(true, "Y3Vyc29yOnYyOpK0MjAyNi0wOS0yMFQxMjoxODozN1rPAAAAARE3rZU=", mergedPull, openPull),
	}})

	listed, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: weave, Cursor: "old"})
	require.NoError(t, err)
	seen := fake.last(t)
	assert.Equal(t, "ListUpdated", seen.Operation)
	assert.Equal(t, listUpdatedQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"owner": "harrisoncramer",
		"name":  "weave",
		"first": float64(100),
		"after": "old",
	}, seen.Variables)
	assert.Len(t, listed.PullRequests, 2)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "Y3Vyc29yOnYyOpK0MjAyNi0wOS0yMFQxMjoxODozN1rPAAAAARE3rZU=", listed.Cursor)
	assert.True(t, listed.RateLimit.Known)
	assert.Equal(t, 4321, listed.RateLimit.Remaining)
	assert.Equal(t, time.Unix(1789900000, 0).UTC(), listed.RateLimit.ResetAt.UTC())
}

// TestListChanged covers the search: the query string over every repository since the
// moment given, the page and cursor, and each row carrying its decision and checks.
func TestListChanged(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ListChanged": {body: searchPage}})

	since := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	listed, err := f.ListChanged(t.Context(), forge.ListChangedParams{
		Repos: []git.RemoteURL{{Host: "github.com", Path: "acme/widgets"}, weave},
		Since: since,
	})
	require.NoError(t, err)
	seen := fake.last(t)
	assert.Equal(t, "ListChanged", seen.Operation)
	assert.Equal(t, listChangedQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"query": "is:pr repo:acme/widgets repo:harrisoncramer/weave updated:>=2026-09-18T12:00:00Z sort:updated-desc",
		"first": float64(100),
		"after": nil,
	}, seen.Variables)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "Y3Vyc29yOjM=", listed.Cursor)
	require.Len(t, listed.PullRequests, 3)
	assert.Equal(t, []int{744, 743, 742}, []int{listed.PullRequests[0].Number, listed.PullRequests[1].Number, listed.PullRequests[2].Number})
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionNone), listed.PullRequests[0].Decision, "a null decision is still an answer")
	assert.Equal(t, xptr.Ptr(forge.ChecksNone), listed.PullRequests[0].Checks, "a row with no commits carries none")
	assert.Equal(t, "https://github.com/harrisoncramer/weave", listed.PullRequests[0].Repository.Canonical())

	f, _, _ = serve(t, map[string]answer{"ListChanged": {body: searchFailing}})
	listed, err = f.ListChanged(t.Context(), forge.ListChangedParams{
		Repos: []git.RemoteURL{weave},
		Since: since,
	})
	require.NoError(t, err)
	require.Len(t, listed.PullRequests, 1)
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionPending), listed.PullRequests[0].Decision)
	assert.Equal(t, xptr.Ptr(forge.ChecksFailing), listed.PullRequests[0].Checks)
	assert.Equal(t, xptr.Ptr(forge.MergeabilityUnstable), listed.PullRequests[0].Mergeability)

	f, _, fake = serve(t, map[string]answer{"ListChanged": {body: searchApproved}})
	listed, err = f.ListChanged(t.Context(), forge.ListChangedParams{
		Repos:  []git.RemoteURL{{Host: "github.com", Path: "acme/deploy"}},
		Since:  since,
		Cursor: "Y3Vyc29yOjM=",
	})
	require.NoError(t, err)
	assert.Equal(t, "Y3Vyc29yOjM=", fake.last(t).Variables["after"])
	assert.False(t, listed.HasMore)
	require.Len(t, listed.PullRequests, 2)
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionApproved), listed.PullRequests[0].Decision)
}

// TestGet covers reading one pull request by its web address.
func TestGet(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Get": {body: data(`{"repository":{"pullRequest":` + mergedPull + `}}`)}})

	pullRequest, err := f.Get(t.Context(), "https://github.com/harrisoncramer/weave/pull/742")
	require.NoError(t, err)
	assert.Equal(t, 742, pullRequest.Number)
	assert.Equal(t, "feat(pullrequest): draw pull request marks in the panel", pullRequest.Title)
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "weave", "number": float64(742)}, fake.last(t).Variables)
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
	_, err = f.ListChanged(t.Context(), forge.ListChangedParams{Repos: []git.RemoteURL{weave, {Host: "github.com", Path: "group/sub/repo"}}})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

// TestUnauthorizedInvalidatesTheCredential covers a refused token: GitHub's 401 reaches the
// Authenticator so the connection reads as unverified.
func TestUnauthorizedInvalidatesTheCredential(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, map[string]answer{"List": {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`}})

	_, err := f.List(t.Context(), forge.ListParams{Repo: weave, Branch: "main"})
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
				_, err := f.Get(t.Context(), "https://github.com/harrisoncramer/weave-forge-fixture/pull/999")

				return err
			},
		},
		{
			name: "a repository the listing cannot see",
			body: `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],"locations":[{"line":2,"column":3}],"message":"Could not resolve to a Repository with the name 'harrisoncramer/no-such-repository-xyz'."}]}`,
			call: func(f *Forge) error {
				_, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: weave})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, auth, _ := serve(t, map[string]answer{"Get": {body: tt.body}, "ListUpdated": {body: tt.body}})
			require.ErrorIs(t, tt.call(f), forge.ErrNotFound)
			assert.Equal(t, 0, auth.invalidated)
		})
	}

	misrouted, _, _ := serve(t, nil)
	_, err := misrouted.Get(t.Context(), "https://github.com/harrisoncramer/weave/pull/1")
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrNotFound)
	assert.Contains(t, err.Error(), "/graphql")
}

// TestVerifyInstallation covers the one REST call: how many repositories an installation
// token reaches, and a refusal reaching the Authenticator.
func TestVerifyInstallation(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"/installation/repositories": {body: `{"total_count":4,"repositories":[{"full_name":"harrisoncramer/weave"}]}`}})

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

			f, _, _ := serve(t, map[string]answer{"ListUpdated": {
				status: tt.status,
				header: tt.header,
				body:   tt.body,
			}})

			_, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: weave})
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

// TestStandings covers the batch: one query aliasing every number, each decision and rollup
// GitHub answers in weave's words with the head they were read for, a null decision and a
// null rollup as none, and a number GitHub does not have left out rather than failing the rest.
func TestStandings(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Standings": {body: standingsBatch}})

	standings, err := f.Standings(t.Context(), weave, []int{801, 802, 803, 1, 9999999})
	require.NoError(t, err)
	assert.Equal(t, map[int]forge.Standing{
		801: {Decision: forge.ReviewDecisionChangesRequested, Checks: forge.ChecksFailing, Mergeability: forge.MergeabilityUnstable, HeadSHA: "1111111111111111111111111111111111111111"},
		802: {Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPending, Mergeability: forge.MergeabilityBlocked, HeadSHA: "2222222222222222222222222222222222222222"},
		803: {Decision: forge.ReviewDecisionApproved, Checks: forge.ChecksPassing, Mergeability: forge.MergeabilityClean, HeadSHA: "3333333333333333333333333333333333333333"},
		1:   {Decision: forge.ReviewDecisionNone, Checks: forge.ChecksNone, Mergeability: forge.MergeabilityUnknown, HeadSHA: "c744e226f1248223edd7aa77a7874aba2eec3b16"},
	}, standings)

	seen := fake.last(t)
	assert.Equal(t, "Standings", seen.Operation)
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "weave"}, seen.Variables)
	assert.Contains(t, seen.Query, `p801: pullRequest(number: 801) { headRefOid reviewDecision mergeStateStatus commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } }`)
	assert.Contains(t, seen.Query, `p9999999: pullRequest(number: 9999999) { headRefOid `)
	assert.Equal(t, 5, strings.Count(seen.Query, "pullRequest(number:"))
}

// TestChecksOf covers every rollup state GitHub documents, and a rollup absent from the answer.
func TestChecksOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rollup *rollupNode
		want   forge.Checks
	}{
		{name: "success is passing", rollup: &rollupNode{State: "SUCCESS"}, want: forge.ChecksPassing},
		{name: "failure is failing", rollup: &rollupNode{State: "FAILURE"}, want: forge.ChecksFailing},
		{name: "error is failing", rollup: &rollupNode{State: "ERROR"}, want: forge.ChecksFailing},
		{name: "pending is pending", rollup: &rollupNode{State: "PENDING"}, want: forge.ChecksPending},
		{name: "expected is pending", rollup: &rollupNode{State: "EXPECTED"}, want: forge.ChecksPending},
		{name: "no rollup is none", rollup: nil, want: forge.ChecksNone},
		{name: "a state weave does not know is none", rollup: &rollupNode{State: "SOMETHING_NEW"}, want: forge.ChecksNone},
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

// TestStandingsRefusals covers a repository GitHub does not have, a path it cannot address, and
// more numbers than one call takes, the last two costing no request.
func TestStandingsRefusals(t *testing.T) {
	t.Parallel()

	missing, _, _ := serve(t, map[string]answer{"Standings": {body: `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],"locations":[{"line":2,"column":3}],"message":"Could not resolve to a Repository with the name 'harrisoncramer/gone'."}]}`}})
	_, err := missing.Standings(t.Context(), git.RemoteURL{Host: "github.com", Path: "harrisoncramer/gone"}, []int{1})
	require.ErrorIs(t, err, forge.ErrNotFound)

	f, _, fake := serve(t, nil)
	_, err = f.Standings(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, []int{1})
	require.ErrorIs(t, err, forge.ErrNotRepository)

	numbers := make([]int, forge.MaxStandings+1)
	for i := range numbers {
		numbers[i] = i + 1
	}
	_, err = f.Standings(t.Context(), weave, numbers)
	require.Error(t, err)
	assert.Empty(t, fake.calls)
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

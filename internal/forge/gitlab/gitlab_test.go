package gitlab

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
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures are what gitlab.com answered anonymously on 20 September 2026 for
// acme/widgets: !256538 open, !254619 a draft, !256531 merged, !256485 closed from a fork,
// a batch of decisions with one iid GitLab does not have, and the refusal three projects at a
// hundred rows each draw.

const openMergeRequest = `{"id":"gid://gitlab/MergeRequest/535736457","iid":"256538","title":"Strip undeclared keys from the settings","webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/256538","state":"opened","draft":false,"createdAt":"2026-09-20T08:58:11Z","updatedAt":"2026-09-20T17:54:15Z","mergedAt":null,"closedAt":null,"sourceBranch":"settings-cleanup","targetBranch":"master","diffHeadSha":"14a5732c18c7186fb0b34d469255b9609e4d11f2","sourceProject":{"webUrl":"https://gitlab.com/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"4aabd419a0202362695a5e4147df061fbf976c9f"},"approvalsRequired":4,"approvalsLeft":4,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"UNREVIEWED"}}]}}`

const draftMergeRequest = `{"id":"gid://gitlab/MergeRequest/530185576","iid":"254619","title":"Draft: Define the palette lifecycle events","webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/254619","state":"opened","draft":true,"createdAt":"2026-09-09T22:53:57Z","updatedAt":"2026-09-20T17:55:51Z","mergedAt":null,"closedAt":null,"sourceBranch":"palette-events","targetBranch":"master","diffHeadSha":"b39c0f269434b23d22d29a74d6a45e749de60368","sourceProject":{"webUrl":"https://gitlab.com/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"70c3fc3551da5b2cc071a20f516ce336128c199b"},"approvalsRequired":4,"approvalsLeft":4,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"UNREVIEWED"}}]}}`

const mergedMergeRequest = `{"id":"gid://gitlab/MergeRequest/535718519","iid":"256531","title":"Update the runtime data for the e2e tests","webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/256531","state":"merged","draft":false,"createdAt":"2026-09-20T05:24:39Z","updatedAt":"2026-09-20T06:24:05Z","mergedAt":"2026-09-20T06:24:05Z","closedAt":null,"sourceBranch":"qa-report-update","targetBranch":"master","diffHeadSha":"f7ae81a86c306ee75b2703ea62137b787950f3f2","sourceProject":{"webUrl":"https://gitlab.com/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"fb3b0f26554913f30d92e12c39b1814eda5b9a63"}}`

const forkMergeRequest = `{"id":"gid://gitlab/MergeRequest/535500829","iid":"256485","title":"Draft: feat(reports): one template for the blockers page","webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/256485","state":"closed","draft":true,"createdAt":"2026-09-18T21:41:21Z","updatedAt":"2026-09-20T17:09:26Z","mergedAt":null,"closedAt":"2026-09-19T20:09:45Z","sourceBranch":"blockers-page-single-template","targetBranch":"master","diffHeadSha":"d636bcb279bab23588cf8973100bc7e6387e9b54","sourceProject":{"webUrl":"https://gitlab.com/acme-forks/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"859512950cbe65e422c71a10bdcbfd9c929769aa"}}`

// standingsBatch is the shape GitLab answers the aliased standings query with, recorded on 21
// September against acme/widgets: a running pipeline, an approved merge request whose
// pipeline passed, one asked for changes whose pipeline failed, a skipped pipeline, one with
// no pipeline, and an iid GitLab does not have.
const standingsBatch = `{"data":{"project":{"m256666":{"detailedMergeStatus":"NOT_APPROVED","diffHeadSha":"834a90786a4b4f6e35100f1f588c56df17a02887","approvalsRequired":0,"approvalsLeft":0,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEW_STARTED"}}]},"headPipeline":{"status":"RUNNING"}},"m255110":{"detailedMergeStatus":"MERGEABLE","diffHeadSha":"b90fb22355180c048573dec4dbdc0c82ef92e31b","approvalsRequired":2,"approvalsLeft":0,"approvedBy":{"count":3},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"APPROVED"}},{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"APPROVED"}}]},"headPipeline":{"status":"SUCCESS"}},"m252724":{"detailedMergeStatus":"CONFLICT","diffHeadSha":"2586500f614ad3dc5c9ee538eaaa0303aa8f076a","approvalsRequired":0,"approvalsLeft":0,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"UNAPPROVED"}},{"mergeRequestInteraction":{"reviewState":"REQUESTED_CHANGES"}},{"mergeRequestInteraction":{"reviewState":"UNAPPROVED"}}]},"headPipeline":{"status":"FAILED"}},"m256649":{"detailedMergeStatus":"DRAFT_STATUS","diffHeadSha":"66fb1f0ba5fe77bb0932daa6c0f3f3f185cac41c","approvalsRequired":0,"approvalsLeft":0,"approvedBy":{"count":0},"reviewers":{"nodes":[]},"headPipeline":{"status":"SKIPPED"}},"m254626":{"detailedMergeStatus":"UNCHECKED","diffHeadSha":"0abc488fc8123613c802cac85f1f7d5c4f9d0725","approvalsRequired":4,"approvalsLeft":4,"approvedBy":{"count":1},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"APPROVED"}},{"mergeRequestInteraction":{"reviewState":"UNAPPROVED"}},{"mergeRequestInteraction":{"reviewState":"UNAPPROVED"}}]},"headPipeline":null},"m999999999":null},"queryComplexity":{"score":118,"limit":200}}}`

const tooComplex = `{"errors":[{"message":"Query has complexity of 294, which exceeds max complexity of 200"}]}`

type fakeAuth struct {
	token       string
	invalidated int
}

func (f *fakeAuth) Credential(context.Context) (forge.Credential, error) {
	return forge.Credential{Header: forge.HeaderPrivateToken, Token: forge.NewSecret(f.token)}, nil
}

func (f *fakeAuth) Invalidate(context.Context) error {
	f.invalidated++

	return nil
}

// call is one request the fake GitLab saw.
type call struct {
	Path      string
	Method    string
	Operation string
	Query     string
	Variables map[string]any
	Header    http.Header
}

// answer is what the fake GitLab says to one operation or REST path.
type answer struct {
	status int
	header map[string]string
	body   string
}

// fakeGitLab answers each named GraphQL operation, and each REST path, with one body, or
// whatever handle says when it is set, and records every request it saw.
type fakeGitLab struct {
	answers map[string]answer
	handle  func(seen call) (answer, bool)
	calls   []call
}

func (g *fakeGitLab) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seen := call{Path: r.URL.EscapedPath(), Method: r.Method, Header: r.Header}
	key := seen.Path
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
	if g.handle != nil {
		reply, ok = g.handle(seen)
	}
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
func (g *fakeGitLab) last(t *testing.T) call {
	t.Helper()
	require.NotEmpty(t, g.calls)

	return g.calls[len(g.calls)-1]
}

// serve is a GitLab answering each operation with one body, recording the requests it saw.
func serve(t *testing.T, answers map[string]answer) (*Forge, *fakeAuth, *fakeGitLab) {
	t.Helper()

	fake := &fakeGitLab{answers: answers}
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

// listing is a page of a project's mergeRequests connection holding the nodes.
func listing(hasNextPage bool, cursor string, nodes ...string) string {
	return `{"data":{"project":{"mergeRequests":` + page(hasNextPage, cursor, nodes...) + `}}}`
}

func page(hasNextPage bool, cursor string, nodes ...string) string {
	next := "false"
	if hasNextPage {
		next = "true"
	}

	return `{"pageInfo":{"hasNextPage":` + next + `,"endCursor":"` + cursor + `"},"nodes":[` + strings.Join(nodes, ",") + `]}`
}

var komrad = git.RemoteURL{
	Raw:    "git@gitlab.com:harrisoncramer/komrad.git",
	Scheme: "ssh",
	Host:   "gitlab.com",
	Path:   "harrisoncramer/komrad",
}

var gitlabOrg = git.RemoteURL{
	Raw:    "https://gitlab.com/acme/widgets",
	Scheme: "https",
	Host:   "gitlab.com",
	Path:   "acme/widgets",
}

// TestList covers the branch listing: the project by its full path, the source branch filter
// with no state, the token in GitLab's own header, and every row mapped into the neutral record
// with the global id cut to its number and a fork's project named.
func TestList(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"List": {body: listing(false, "end", openMergeRequest, mergedMergeRequest, forkMergeRequest, draftMergeRequest)},
	})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    gitlabOrg,
		Branch:  "settings-cleanup",
		Cursor:  "c2",
		PerPage: 50,
	})
	require.NoError(t, err)

	seen := fake.last(t)
	assert.Equal(t, "List", seen.Operation)
	assert.Equal(t, listQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"path":     "acme/widgets",
		"first":    float64(50),
		"after":    "c2",
		"branches": []any{"settings-cleanup"},
	}, seen.Variables)
	assert.Equal(t, "secret", seen.Header.Get("PRIVATE-TOKEN"))
	assert.False(t, listed.HasMore)
	assert.Equal(t, "end", listed.Cursor)
	pullRequests := listed.PullRequests
	require.Len(t, pullRequests, 4)

	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: "535736457",
		URL:        "https://gitlab.com/acme/widgets/-/merge_requests/256538",
		Repository: gitlabOrg,
		Head:       gitlabOrg,
		Number:     256538,
		Title:      "Strip undeclared keys from the settings",
		HeadBranch: "settings-cleanup",
		HeadSHA:    "14a5732c18c7186fb0b34d469255b9609e4d11f2",
		BaseBranch: "master",
		BaseSHA:    "4aabd419a0202362695a5e4147df061fbf976c9f",
		State:      "OPEN",
		CreatedAt:  time.Date(2026, 9, 20, 8, 58, 11, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 20, 17, 54, 15, 0, time.UTC),
	}, pullRequests[0])
	assert.Nil(t, pullRequests[0].Decision, "a listing does not ask for the decision")

	stamp := time.Date(2026, 9, 20, 6, 24, 5, 0, time.UTC)
	assert.Equal(t, "535718519", pullRequests[1].ProviderID)
	assert.Equal(t, "MERGED", pullRequests[1].State)
	assert.Equal(t, &stamp, pullRequests[1].MergedAt)
	assert.Equal(t, &stamp, pullRequests[1].ClosedAt, "a merge closes the merge request too")
	assert.True(t, pullRequests[1].IsFinished())

	assert.Equal(t, "CLOSED", pullRequests[2].State)
	assert.Equal(t, "acme-forks/acme/widgets", pullRequests[2].Head.Path)
	assert.True(t, pullRequests[2].IsFinished())

	assert.True(t, pullRequests[3].Draft)
}

// TestListWithoutABranch covers the project's own listing: open ones only, no branch filter.
func TestListWithoutABranch(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"List": {body: listing(false, "", openMergeRequest)}})

	_, err := f.List(t.Context(), forge.ListParams{Repo: komrad, PerPage: 20})
	require.NoError(t, err)

	seen := fake.last(t)
	assert.Equal(t, "opened", seen.Variables["state"])
	assert.NotContains(t, seen.Variables, "branches")
	assert.Nil(t, seen.Variables["after"])
}

// TestListReadsTheNextPage covers GitLab saying another page follows, and where it starts.
func TestListReadsTheNextPage(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, map[string]answer{"List": {body: listing(true, "eyJjcmVhdGVk", openMergeRequest)}})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    komrad,
		Branch:  "agent/x",
		PerPage: 1,
	})
	require.NoError(t, err)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "eyJjcmVhdGVk", listed.Cursor)
}

// TestListAddressesANestedGroup covers a project under a subgroup, which GraphQL takes as its
// full path.
func TestListAddressesANestedGroup(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"List": {body: listing(false, "")}})

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "gitlab.com", Path: "group/sub/repo"}, Branch: "main"})
	require.NoError(t, err)
	assert.Equal(t, "group/sub/repo", fake.last(t).Variables["path"])
}

// TestListUpdated covers the sweep's request: every state by last change, the largest page,
// the cursor carried in and out, and the rate limit read off GitLab's headers.
func TestListUpdated(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ListUpdated": {
		header: map[string]string{"RateLimit-Remaining": "1999", "RateLimit-Reset": "1789900000"},
		body:   listing(true, "eyJ1cGRhdGVk", openMergeRequest, mergedMergeRequest),
	}})

	listed, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: komrad, Cursor: "old"})
	require.NoError(t, err)
	seen := fake.last(t)
	assert.Equal(t, "ListUpdated", seen.Operation)
	assert.Equal(t, listUpdatedQuery, seen.Query)
	assert.Equal(t, map[string]any{
		"path":  "harrisoncramer/komrad",
		"first": float64(100),
		"after": "old",
	}, seen.Variables)
	assert.Len(t, listed.PullRequests, 2)
	assert.True(t, listed.HasMore)
	assert.Equal(t, "eyJ1cGRhdGVk", listed.Cursor)
	assert.True(t, listed.RateLimit.Known)
	assert.Equal(t, 1999, listed.RateLimit.Remaining)
	assert.Equal(t, time.Unix(1789900000, 0).UTC(), listed.RateLimit.ResetAt.UTC())
}

// TestListChanged covers the aliased query: every project in one request with its own
// cursor, the rows of each with their decisions, a project GitLab has nothing for skipped, and
// the cursor carrying only the projects with a page left.
func TestListChanged(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"ListChanged": {body: `{"data":{` +
		`"r0":{"mergeRequests":` + page(true, "eyJ1cGRhdGVk", openMergeRequest) + `},` +
		`"r1":null,` +
		`"queryComplexity":{"score":100,"limit":200}}}`}})

	since := time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC)
	listed, err := f.ListChanged(t.Context(), forge.ListChangedParams{
		Repos: []git.RemoteURL{komrad, gitlabOrg},
		Since: since,
	})
	require.NoError(t, err)
	seen := fake.last(t)
	assert.Equal(t, "ListChanged", seen.Operation)
	assert.Equal(t, map[string]any{
		"since": "2026-09-20T17:00:00Z",
		"first": float64(100),
		"p0":    "acme/widgets",
		"a0":    nil,
		"p1":    "harrisoncramer/komrad",
		"a1":    nil,
	}, seen.Variables)
	assert.Contains(t, seen.Query, "r0: project(fullPath: $p0) { mergeRequests(updatedAfter: $since, sort: UPDATED_DESC, first: $first, after: $a0)")
	assert.Contains(t, seen.Query, "queryComplexity { score limit }")
	assert.Contains(t, seen.Query, standingFields)
	require.Len(t, listed.PullRequests, 1)
	assert.Equal(t, 256538, listed.PullRequests[0].Number)
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionPending), listed.PullRequests[0].Decision, "four approvals owed is pending")
	assert.Equal(t, xptr.Ptr(forge.ChecksNone), listed.PullRequests[0].Checks, "a row with no pipeline carries none")
	assert.True(t, listed.HasMore)
	assert.JSONEq(t, `{"acme/widgets":"eyJ1cGRhdGVk"}`, listed.Cursor)

	f, _, fake = serve(t, map[string]answer{"ListChanged": {body: `{"data":{"r0":{"mergeRequests":` + page(false, "", mergedMergeRequest) + `},"queryComplexity":{"score":52,"limit":200}}}`}})
	listed, err = f.ListChanged(t.Context(), forge.ListChangedParams{
		Repos:  []git.RemoteURL{komrad, gitlabOrg},
		Since:  since,
		Cursor: `{"acme/widgets":"eyJ1cGRhdGVk"}`,
	})
	require.NoError(t, err)
	seen = fake.last(t)
	assert.Equal(t, "acme/widgets", seen.Variables["p0"])
	assert.Equal(t, "eyJ1cGRhdGVk", seen.Variables["a0"])
	assert.NotContains(t, seen.Variables, "p1", "only the project with a page left is asked again")
	assert.False(t, listed.HasMore)
	assert.Empty(t, listed.Cursor)
	require.Len(t, listed.PullRequests, 1)
	assert.Equal(t, xptr.Ptr(forge.ReviewDecisionNone), listed.PullRequests[0].Decision)
}

// TestListChangedSplitsOnComplexity covers GitLab refusing too many projects in one query: the
// batch is halved until it fits, and what fits sizes the batches after it.
func TestListChangedSplitsOnComplexity(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)
	fake.handle = func(seen call) (answer, bool) {
		projects := strings.Count(seen.Query, "project(fullPath:")
		if projects > 1 {
			return answer{body: tooComplex}, true
		}

		return answer{body: `{"data":{"r0":{"mergeRequests":` + page(false, "", openMergeRequest) + `},"queryComplexity":{"score":100,"limit":100}}}`}, true
	}

	listed, err := f.ListChanged(t.Context(), forge.ListChangedParams{
		Repos: []git.RemoteURL{{Host: "gitlab.com", Path: "a/one"}, {Host: "gitlab.com", Path: "a/two"}, {Host: "gitlab.com", Path: "a/three"}},
		Since: time.Date(2026, 9, 20, 17, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Len(t, listed.PullRequests, 3, "every project is read once the batch fits")

	batches := xslice.Map(fake.calls, func(seen call) int {
		return strings.Count(seen.Query, "project(fullPath:")
	})
	assert.Equal(t, []int{2, 1, 1, 1}, batches, "two are refused, then one at a time since one filled the limit")
}

// TestListChangedRefusesABadCursor covers a cursor this forge did not hand out.
func TestListChangedRefusesABadCursor(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	_, err := f.ListChanged(t.Context(), forge.ListChangedParams{Repos: []git.RemoteURL{komrad}, Cursor: "Y3Vyc29yOjM="})
	require.Error(t, err)
	assert.Empty(t, fake.calls)
}

// TestGet covers one merge request by its web address.
func TestGet(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Get": {body: `{"data":{"project":{"mergeRequest":` + forkMergeRequest + `}}}`}})

	pullRequest, err := f.Get(t.Context(), "https://gitlab.com/acme/widgets/-/merge_requests/256485")
	require.NoError(t, err)
	assert.Equal(t, 256485, pullRequest.Number)
	assert.Equal(t, "Draft: feat(reports): one template for the blockers page", pullRequest.Title)
	assert.Equal(t, map[string]any{"path": "acme/widgets", "iid": "256485"}, fake.last(t).Variables)
}

// TestMerge covers accepting a merge request over REST and GitLab's refusals.
func TestMerge(t *testing.T) {
	t.Parallel()

	const (
		address = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/7"
		path    = "/projects/harrisoncramer%2Fkomrad/merge_requests/7"
	)

	refused := answer{status: http.StatusMethodNotAllowed, body: `{"message":"405 Method Not Allowed"}`}

	tests := []struct {
		name        string
		answers     map[string]answer
		wantErr     error
		wantMessage string
	}{
		{
			name:    "the merge request is accepted",
			answers: map[string]answer{path + "/merge": {body: `{"iid":7,"state":"merged"}`}},
		},
		{
			name: "a merge request that has already merged",
			answers: map[string]answer{
				path + "/merge": refused,
				path:            {body: `{"state":"merged","detailed_merge_status":"not_open"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab will not merge !7: it has already merged",
		},
		{
			name: "a merge request with conflicts",
			answers: map[string]answer{
				path + "/merge": {status: http.StatusNotAcceptable, body: `{"message":"Branch cannot be merged"}`},
				path:            {body: `{"state":"opened","detailed_merge_status":"conflict"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab will not merge !7: it has merge conflicts",
		},
		{
			name: "a merge request gitlab is still checking",
			answers: map[string]answer{
				path + "/merge": refused,
				path:            {body: `{"state":"opened","detailed_merge_status":"checking"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab is still checking whether it can merge",
		},
		{
			name: "a draft gitlab is rechecking",
			answers: map[string]answer{
				path + "/merge": refused,
				path:            {body: `{"state":"opened","draft":true,"detailed_merge_status":"checking"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab will not merge !7: it is still a draft",
		},
		{
			name: "a merge status komrad has no words for",
			answers: map[string]answer{
				path + "/merge": refused,
				path:            {body: `{"state":"opened","detailed_merge_status":"locked_paths"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "its merge status is locked paths",
		},
		{
			name:        "a refusal gitlab will not explain",
			answers:     map[string]answer{path + "/merge": refused},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "it did not say why",
		},
		{
			name:    "a credential gitlab will not let merge",
			answers: map[string]answer{path + "/merge": {status: http.StatusForbidden, body: `{"message":"403 Forbidden"}`}},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:    "a merge request gitlab does not have",
			wantErr: forge.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.Merge(t.Context(), address)
			assert.Equal(t, http.MethodPut, fake.calls[0].Method)
			assert.Equal(t, path+"/merge", fake.calls[0].Path)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), tt.wantMessage)

				return
			}
			require.NoError(t, err)
		})
	}
}

// TestGetRefusesWhatIsNotAMergeRequest covers the addresses Get will not ask GitLab about.
func TestGetRefusesWhatIsNotAMergeRequest(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	tests := []struct {
		name    string
		address string
	}{
		{name: "a pull request path", address: "https://github.com/owner/repo/pull/1"},
		{name: "an issue", address: "https://gitlab.com/owner/repo/-/issues/1"},
		{name: "no number", address: "https://gitlab.com/owner/repo/-/merge_requests/"},
		{name: "a project", address: "https://gitlab.com/owner/repo"},
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

// TestListRefusesABarePath covers a repository path GitLab cannot address.
func TestListRefusesABarePath(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "gitlab.com", Path: "repo"}, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	_, err = f.ListChanged(t.Context(), forge.ListChangedParams{Repos: []git.RemoteURL{komrad, {Host: "gitlab.com", Path: "repo"}}})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

// TestVerify covers asking GitLab who the credential is, a token GitLab refuses reaching the
// Authenticator, and a token that reaches nobody.
func TestVerify(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Verify": {body: `{"data":{"currentUser":{"username":"harrisoncramer"}}}`}})

	login, err := f.Verify(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
	assert.Equal(t, verifyQuery, fake.last(t).Query)

	refused, auth, _ := serve(t, map[string]answer{"Verify": {status: http.StatusUnauthorized, body: `{"message":"401 Unauthorized"}`}})
	_, err = refused.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)

	nobody, _, _ := serve(t, map[string]answer{"Verify": {body: `{"data":{"currentUser":null}}`}})
	_, err = nobody.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
}

// TestNotFound covers a project or merge request GitLab does not have, or will not show, which
// it answers as null with no error, and a host whose GraphQL is somewhere else.
func TestNotFound(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, map[string]answer{
		"Get":         {body: `{"data":{"project":null}}`},
		"ListUpdated": {body: `{"data":{"project":null}}`},
		"Standings":   {body: `{"data":{"project":null}}`},
	})

	_, err := f.Get(t.Context(), "https://gitlab.com/acme/no-such-project-xyz/-/merge_requests/1")
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: komrad})
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = f.Standings(t.Context(), komrad, []int{1})
	require.ErrorIs(t, err, forge.ErrNotFound)
	assert.Equal(t, 0, auth.invalidated)

	gone, _, _ := serve(t, map[string]answer{"Get": {body: `{"data":{"project":{"mergeRequest":null}}}`}})
	_, err = gone.Get(t.Context(), "https://gitlab.com/acme/widgets/-/merge_requests/999999999")
	require.ErrorIs(t, err, forge.ErrNotFound)

	misrouted, _, _ := serve(t, nil)
	_, err = misrouted.Get(t.Context(), "https://gitlab.com/acme/widgets/-/merge_requests/1")
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrNotFound)
	assert.Contains(t, err.Error(), "/graphql")
}

// TestRateLimitRefusals covers a 429 with and without a time to wait, and a 403 that is a
// permission answer rather than a throttle.
func TestRateLimitRefusals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		status        int
		header        map[string]string
		wantThrottled bool
		wantRetryAt   time.Time
	}{
		{
			name:   "a 403 is a permission answer",
			status: http.StatusForbidden,
		},
		{
			name:          "retry-after says how long",
			status:        http.StatusTooManyRequests,
			header:        map[string]string{"Retry-After": "30"},
			wantThrottled: true,
			wantRetryAt:   now.Add(30 * time.Second),
		},
		{
			name:          "a spent budget says when it resets",
			status:        http.StatusTooManyRequests,
			header:        map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "1789900000"},
			wantThrottled: true,
			wantRetryAt:   time.Unix(1789900000, 0),
		},
		{
			name:          "a 429 with nothing else is a refusal with no time",
			status:        http.StatusTooManyRequests,
			wantThrottled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, map[string]answer{"ListUpdated": {
				status: tt.status,
				header: tt.header,
				body:   `{"message":"refused"}`,
			}})

			_, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: komrad})
			require.Error(t, err)

			var throttled *forge.ThrottledError
			if !tt.wantThrottled {
				assert.NotErrorAs(t, err, &throttled)

				return
			}

			require.ErrorAs(t, err, &throttled)
			assert.True(t, throttled.RetryAt.Equal(tt.wantRetryAt), "retry at %s, want %s", throttled.RetryAt, tt.wantRetryAt)
		})
	}
}

// TestStandings covers the batch: one query aliasing every iid, each folded into komrad's
// words with the head it was read for, and an iid GitLab does not have left out rather than
// failing the rest.
func TestStandings(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{"Standings": {body: standingsBatch}})

	numbers := []int{256666, 255110, 252724, 256649, 254626, 999999999}
	standings, err := f.Standings(t.Context(), gitlabOrg, numbers)
	require.NoError(t, err)
	assert.Equal(t, map[int]forge.Standing{
		256666: {
			Decision:     forge.ReviewDecisionNone,
			Checks:       forge.ChecksPending,
			Mergeability: forge.MergeabilityBlocked,
			HeadSHA:      "834a90786a4b4f6e35100f1f588c56df17a02887",
		},
		255110: {
			Decision:     forge.ReviewDecisionApproved,
			Checks:       forge.ChecksPassing,
			Mergeability: forge.MergeabilityClean,
			HeadSHA:      "b90fb22355180c048573dec4dbdc0c82ef92e31b",
		},
		252724: {
			Decision:     forge.ReviewDecisionChangesRequested,
			Checks:       forge.ChecksFailing,
			Mergeability: forge.MergeabilityConflicting,
			HeadSHA:      "2586500f614ad3dc5c9ee538eaaa0303aa8f076a",
		},
		256649: {
			Decision:     forge.ReviewDecisionNone,
			Checks:       forge.ChecksNone,
			Mergeability: forge.MergeabilityBlocked,
			HeadSHA:      "66fb1f0ba5fe77bb0932daa6c0f3f3f185cac41c",
		},
		254626: {
			Decision:     forge.ReviewDecisionPending,
			Checks:       forge.ChecksNone,
			Mergeability: forge.MergeabilityUnknown,
			HeadSHA:      "0abc488fc8123613c802cac85f1f7d5c4f9d0725",
		},
	}, standings)

	seen := fake.last(t)
	assert.Equal(t, "Standings", seen.Operation)
	assert.Equal(t, map[string]any{"path": "acme/widgets"}, seen.Variables)
	assert.Contains(t, seen.Query, `m256666: mergeRequest(iid: "256666") { diffHeadSha `+standingFields+` }`)
	assert.Contains(t, seen.Query, "queryComplexity { score limit }")
	assert.Equal(t, 6, strings.Count(seen.Query, "mergeRequest(iid:"))
}

// TestMergeabilityFolds covers the detailed merge statuses GitLab documents: one word each for
// mergeable, checks in the way, behind and conflicting, unknown while GitLab is still working
// it out, and blocked for every other rule.
func TestMergeabilityFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   forge.Mergeability
	}{
		{status: "MERGEABLE", want: forge.MergeabilityClean},
		{status: "CI_MUST_PASS", want: forge.MergeabilityUnstable},
		{status: "CI_STILL_RUNNING", want: forge.MergeabilityUnstable},
		{status: "COMMITS_STATUS", want: forge.MergeabilityBlocked},
		{status: "NEED_REBASE", want: forge.MergeabilityBehind},
		{status: "CONFLICT", want: forge.MergeabilityConflicting},
		{status: "UNCHECKED", want: forge.MergeabilityUnknown},
		{status: "CHECKING", want: forge.MergeabilityUnknown},
		{status: "PREPARING", want: forge.MergeabilityUnknown},
		{status: "APPROVALS_SYNCING", want: forge.MergeabilityUnknown},
		{status: "", want: forge.MergeabilityUnknown},
		{status: "NOT_APPROVED", want: forge.MergeabilityBlocked},
		{status: "DISCUSSIONS_NOT_RESOLVED", want: forge.MergeabilityBlocked},
		{status: "DRAFT_STATUS", want: forge.MergeabilityBlocked},
		{status: "BLOCKED_STATUS", want: forge.MergeabilityBlocked},
		{status: "POLICIES_DENIED", want: forge.MergeabilityBlocked},
		{status: "REQUESTED_CHANGES", want: forge.MergeabilityBlocked},
	}
	for _, tt := range tests {
		t.Run("status "+tt.status, func(t *testing.T) {
			t.Parallel()

			node := standingNode{DetailedMergeStatus: tt.status}
			assert.Equal(t, tt.want, node.mergeability())
		})
	}
}

// TestChecksFolds covers every pipeline status GitLab documents: a cancelled pipeline is a
// failure, a manual one is still waiting, and a skipped one says nothing about the branch.
func TestChecksFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   forge.Checks
	}{
		{status: "SUCCESS", want: forge.ChecksPassing},
		{status: "FAILED", want: forge.ChecksFailing},
		{status: "CREATED", want: forge.ChecksPending},
		{status: "WAITING_FOR_RESOURCE", want: forge.ChecksPending},
		{status: "PREPARING", want: forge.ChecksPending},
		{status: "WAITING_FOR_CALLBACK", want: forge.ChecksPending},
		{status: "PENDING", want: forge.ChecksPending},
		{status: "RUNNING", want: forge.ChecksPending},
		{status: "SCHEDULED", want: forge.ChecksPending},
		{status: "CANCELED", want: forge.ChecksFailing},
		{status: "CANCELING", want: forge.ChecksFailing},
		{status: "SKIPPED", want: forge.ChecksNone},
		{status: "MANUAL", want: forge.ChecksPending},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			var node standingNode
			require.NoError(t, json.Unmarshal([]byte(`{"headPipeline":{"status":"`+tt.status+`"}}`), &node))
			assert.Equal(t, tt.want, node.checks())
		})
	}

	var node standingNode
	require.NoError(t, json.Unmarshal([]byte(`{"headPipeline":null}`), &node))
	assert.Equal(t, forge.ChecksNone, node.checks())
}

// TestStandingsSplitsOnComplexity covers more numbers than one query takes against a
// GitLab whose limit is lower than the measured fit: a refused batch is halved and never asked
// at that size again, and the score a batch answers sizes the next up to that ceiling.
func TestStandingsSplitsOnComplexity(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)
	fake.handle = func(seen call) (answer, bool) {
		count := strings.Count(seen.Query, "mergeRequest(iid:")
		if count > 8 {
			return answer{body: tooComplex}, true
		}

		var fields []string
		for _, line := range strings.Split(seen.Query, "\n") {
			alias, _, ok := strings.Cut(strings.TrimSpace(line), ":")
			if ok && strings.HasPrefix(alias, "m") {
				fields = append(fields, `"`+alias+`":{"approvalsRequired":1,"approvalsLeft":0,"approvedBy":{"count":1},"reviewers":{"nodes":[]}}`)
			}
		}

		return answer{body: `{"data":{"project":{` + strings.Join(fields, ",") + `},"queryComplexity":{"score":` + strconv.Itoa(15*count) + `,"limit":200}}}`}, true
	}

	numbers := make([]int, 0, forge.MaxStandings)
	for i := range forge.MaxStandings {
		numbers = append(numbers, i+1)
	}
	standings, err := f.Standings(t.Context(), gitlabOrg, numbers)
	require.NoError(t, err)
	assert.Len(t, standings, forge.MaxStandings)
	assert.Equal(t, forge.ReviewDecisionApproved, standings[50].Decision)

	batches := xslice.Map(fake.calls, func(seen call) int {
		return strings.Count(seen.Query, "mergeRequest(iid:")
	})
	assert.Equal(t, []int{10, 5, 9, 4, 8, 8, 8, 8, 8, 1}, batches, "each refusal lowers the ceiling the score may grow the batch back to")
}

// TestStandingsRefusesTooMany covers more numbers than one call takes, and a path
// GitLab cannot address, neither costing a request.
func TestStandingsRefusesTooMany(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

	numbers := make([]int, forge.MaxStandings+1)
	for i := range numbers {
		numbers[i] = i + 1
	}
	_, err := f.Standings(t.Context(), komrad, numbers)
	require.Error(t, err)
	_, err = f.Standings(t.Context(), git.RemoteURL{Host: "gitlab.com", Path: "repo"}, []int{1})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

// TestDecisionFolds covers how the approvals become komrad's word: a reviewer asking for
// changes first, then approvals still owed, then anybody having approved, else nothing.
func TestDecisionFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node string
		want forge.ReviewDecision
	}{
		{
			name: "a reviewer asking for changes beats an approval",
			node: `{"approvalsRequired":1,"approvalsLeft":0,"approvedBy":{"count":1},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REQUESTED_CHANGES"}},{"mergeRequestInteraction":{"reviewState":"APPROVED"}}]}}`,
			want: forge.ReviewDecisionChangesRequested,
		},
		{
			name: "approvals still owed are pending whoever has approved",
			node: `{"approvalsRequired":2,"approvalsLeft":2,"approvedBy":{"count":1},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"UNAPPROVED"}},{"mergeRequestInteraction":{"reviewState":"APPROVED"}},{"mergeRequestInteraction":{"reviewState":"REVIEWED"}}]}}`,
			want: forge.ReviewDecisionPending,
		},
		{
			name: "approved",
			node: `{"approvalsRequired":1,"approvalsLeft":0,"approvedBy":{"count":1},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"APPROVED"}}]}}`,
			want: forge.ReviewDecisionApproved,
		},
		{
			name: "no rule and nobody has approved is nothing, whatever the reviewers did",
			node: `{"approvalsRequired":0,"approvalsLeft":0,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}}]}}`,
			want: forge.ReviewDecisionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var node standingNode
			require.NoError(t, json.Unmarshal([]byte(tt.node), &node))
			assert.Equal(t, tt.want, node.decision())
		})
	}
}

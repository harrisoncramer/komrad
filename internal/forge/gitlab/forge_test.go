package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures are what gitlab.com answered anonymously on 20 September 2026 for
// acme/widgets: !256538 open, !254619 a draft, !256531 merged, !256485 closed from a fork,
// and the refusal a query too complex draws.

const openMergeRequest = `{"id":"gid://gitlab/MergeRequest/535736457","iid":"256538","title":"Strip undeclared keys from the settings","description":"Drops keys the schema does not declare.","author":{"username":"someone"},"webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/256538","state":"opened","draft":false,"createdAt":"2026-09-20T08:58:11Z","updatedAt":"2026-09-20T17:54:15Z","mergedAt":null,"closedAt":null,"sourceBranch":"settings-cleanup","targetBranch":"master","diffHeadSha":"14a5732c18c7186fb0b34d469255b9609e4d11f2","sourceProject":{"webUrl":"https://gitlab.com/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"4aabd419a0202362695a5e4147df061fbf976c9f"},"approvalsRequired":4,"approvalsLeft":4,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"UNREVIEWED"}}]}}`

const draftMergeRequest = `{"id":"gid://gitlab/MergeRequest/530185576","iid":"254619","title":"Draft: Define the palette lifecycle events","webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/254619","state":"opened","draft":true,"createdAt":"2026-09-09T22:53:57Z","updatedAt":"2026-09-20T17:55:51Z","mergedAt":null,"closedAt":null,"sourceBranch":"palette-events","targetBranch":"master","diffHeadSha":"b39c0f269434b23d22d29a74d6a45e749de60368","sourceProject":{"webUrl":"https://gitlab.com/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"70c3fc3551da5b2cc071a20f516ce336128c199b"},"approvalsRequired":4,"approvalsLeft":4,"approvedBy":{"count":0},"reviewers":{"nodes":[{"mergeRequestInteraction":{"reviewState":"REVIEWED"}},{"mergeRequestInteraction":{"reviewState":"UNREVIEWED"}}]}}`

const mergedMergeRequest = `{"id":"gid://gitlab/MergeRequest/535718519","iid":"256531","title":"Update the runtime data for the e2e tests","webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/256531","state":"merged","draft":false,"createdAt":"2026-09-20T05:24:39Z","updatedAt":"2026-09-20T06:24:05Z","mergedAt":"2026-09-20T06:24:05Z","closedAt":null,"sourceBranch":"qa-report-update","targetBranch":"master","diffHeadSha":"f7ae81a86c306ee75b2703ea62137b787950f3f2","sourceProject":{"webUrl":"https://gitlab.com/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"fb3b0f26554913f30d92e12c39b1814eda5b9a63"}}`

const forkMergeRequest = `{"id":"gid://gitlab/MergeRequest/535500829","iid":"256485","title":"Draft: feat(reports): one template for the blockers page","description":null,"author":null,"webUrl":"https://gitlab.com/acme/widgets/-/merge_requests/256485","state":"closed","draft":true,"createdAt":"2026-09-18T21:41:21Z","updatedAt":"2026-09-20T17:09:26Z","mergedAt":null,"closedAt":"2026-09-19T20:09:45Z","sourceBranch":"blockers-page-single-template","targetBranch":"master","diffHeadSha":"d636bcb279bab23588cf8973100bc7e6387e9b54","sourceProject":{"webUrl":"https://gitlab.com/acme-forks/acme/widgets"},"targetProject":{"webUrl":"https://gitlab.com/acme/widgets"},"diffRefs":{"baseSha":"859512950cbe65e422c71a10bdcbfd9c929769aa"}}`

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
	Body      map[string]any
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
	seen := call{
		Path:   r.URL.EscapedPath(),
		Method: r.Method,
		Header: r.Header,
	}
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
	} else {
		_ = json.NewDecoder(r.Body).Decode(&seen.Body)
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

// TestNotFound covers a project or merge request GitLab does not have, or will not show, which
// it answers as null with no error, and a host whose GraphQL is somewhere else.
func TestNotFound(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, map[string]answer{
		"Get":           {body: `{"data":{"project":null}}`},
		"List":          {body: `{"data":{"project":null}}`},
		"ReadBranches":  {body: `{"data":{"project":null}}`},
		"ReviewThreads": {body: `{"data":{"project":null}}`},
	})

	_, err := f.readMergeRequest(t.Context(), "https://gitlab.com/acme/no-such-project-xyz/-/merge_requests/1")
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = f.List(t.Context(), forge.ListParams{Repo: komrad, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = f.ReadBranches(t.Context(), komrad, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = f.ReviewThreads(t.Context(), komrad, 1)
	require.ErrorIs(t, err, forge.ErrNotFound)
	assert.Equal(t, 0, auth.invalidated)

	gone, _, _ := serve(t, map[string]answer{
		"Get":           {body: `{"data":{"project":{"mergeRequest":null}}}`},
		"ReviewThreads": {body: `{"data":{"project":{"mergeRequest":null}}}`},
	})
	_, err = gone.readMergeRequest(t.Context(), "https://gitlab.com/acme/widgets/-/merge_requests/999999999")
	require.ErrorIs(t, err, forge.ErrNotFound)
	_, err = gone.ReviewThreads(t.Context(), komrad, 1)
	require.ErrorIs(t, err, forge.ErrNotFound)

	misrouted, _, _ := serve(t, nil)
	_, err = misrouted.readMergeRequest(t.Context(), "https://gitlab.com/acme/widgets/-/merge_requests/1")
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrNotFound)
	assert.ErrorContains(t, err, "/graphql")
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

			f, _, _ := serve(t, map[string]answer{"ReadBranches": {
				status: tt.status,
				header: tt.header,
				body:   `{"message":"refused"}`,
			}})

			_, err := f.ReadBranches(t.Context(), komrad, []string{"main"})
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

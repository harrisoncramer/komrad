package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures carry the shape GitHub answered on 20 September 2026, with the rows from other
// repositories made up: komrad-forge-fixture#1 open, komrad#742 merged, widgets#55 from a fork,
// widgets#13 from a fork since deleted, widgets#81544 a draft, and the merge readiness fields GitHub
// answers beside each on a read of branches.

const openPull = `{
	"id": "PR_kwDOUgkrbM8AAAABEIdmHw",
	"number": 1,
	"title": "fixture: open",
	"body": "Opens the fixture.",
	"author": {
		"login": "harrisoncramer"
	},
	"url": "https://github.com/harrisoncramer/komrad-forge-fixture/pull/1",
	"state": "OPEN",
	"isDraft": false,
	"createdAt": "2026-09-18T19:14:17Z",
	"updatedAt": "2026-09-18T19:14:17Z",
	"mergedAt": null,
	"closedAt": null,
	"headRefName": "fixture/open",
	"headRefOid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
	"baseRefName": "main",
	"baseRefOid": "8b32d7985e62febbf494e815545c22b446276b5d",
	"headRepository": {
		"url": "https://github.com/harrisoncramer/komrad-forge-fixture"
	},
	"baseRepository": {
		"url": "https://github.com/harrisoncramer/komrad-forge-fixture"
	}
}`

const mergedPull = `{
	"id": "PR_kwDOTgPfPc8AAAABEU5L_Q",
	"number": 742,
	"title": "feat(pullrequest): draw pull request marks in the panel",
	"url": "https://github.com/harrisoncramer/komrad/pull/742",
	"state": "MERGED",
	"isDraft": false,
	"createdAt": "2026-09-20T17:57:28Z",
	"updatedAt": "2026-09-20T18:00:07Z",
	"mergedAt": "2026-09-20T18:00:07Z",
	"closedAt": "2026-09-20T18:00:07Z",
	"headRefName": "agent/01a0bea9-02d2-7ce0-bbb4-1fbfc3e74198",
	"headRefOid": "70b1d987cdd21940203ca3fbc5b3a7998a47c830",
	"baseRefName": "main",
	"baseRefOid": "79a1ad8fe89cca4494a40c5c5015f46458ddc121",
	"headRepository": {
		"url": "https://github.com/harrisoncramer/komrad"
	},
	"baseRepository": {
		"url": "https://github.com/harrisoncramer/komrad"
	}
}`

const forkPull = `{
	"id": "MDExOlB1bGxSZXF1ZXN0MDAwMDAwMDE=",
	"number": 55,
	"title": "Typo correction",
	"url": "https://github.com/acme/widgets/pull/55",
	"state": "CLOSED",
	"isDraft": false,
	"createdAt": "2014-06-11T05:07:11Z",
	"updatedAt": "2014-06-13T12:41:36Z",
	"mergedAt": null,
	"closedAt": "2014-06-11T05:07:37Z",
	"headRefName": "fix-typo",
	"headRefOid": "7a97b155f47ac6fc34eaa630d96ba9caf244d80c",
	"baseRefName": "master",
	"baseRefOid": "fd5d98f21b5008f27513d52ac2faa78292a8a3a8",
	"headRepository": {
		"url": "https://github.com/someone/widgets"
	},
	"baseRepository": {
		"url": "https://github.com/acme/widgets"
	}
}`

const deletedForkPull = `{
	"id": "MDExOlB1bGxSZXF1ZXN0MDAwMDAwMDI=",
	"number": 13,
	"title": "client: add the service tests",
	"body": null,
	"author": null,
	"url": "https://github.com/acme/widgets/pull/13",
	"state": "CLOSED",
	"isDraft": false,
	"createdAt": "2014-06-07T11:41:26Z",
	"updatedAt": "2014-06-20T06:20:42Z",
	"mergedAt": null,
	"closedAt": "2014-06-20T06:20:42Z",
	"headRefName": "client-tests",
	"headRefOid": "2c8de7699fd79407246011a82201718588a06a08",
	"baseRefName": "master",
	"baseRefOid": "ba7fe835ce990977997bfc6b6c914d235d9ae72f",
	"headRepository": null,
	"baseRepository": {
		"url": "https://github.com/acme/widgets"
	}
}`

const draftPull = `{
	"id": "PR_kwDOAAAAAAAAAAABAAAAAA",
	"number": 81544,
	"title": "unicode: widen the pair members",
	"url": "https://github.com/acme/widgets/pull/81544",
	"state": "OPEN",
	"isDraft": true,
	"createdAt": "2026-09-15T17:53:12Z",
	"updatedAt": "2026-09-15T18:47:47Z",
	"mergedAt": null,
	"closedAt": null,
	"headRefName": "widen-pairs",
	"headRefOid": "695f59abb90db534d1ce005c6b2fc1779728d2b1",
	"baseRefName": "master",
	"baseRefOid": "d90cb4e010dbbf7f39b993db5e64e2904c74e4a4",
	"headRepository": {
		"url": "https://github.com/someone/widgets"
	},
	"baseRepository": {
		"url": "https://github.com/acme/widgets"
	}
}`

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
	Body      map[string]any
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
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
		seen.Operation = graphql.OperationOf(posted.Query)
		seen.Query = posted.Query
		seen.Variables = posted.Variables
		key = seen.Operation
	} else {
		if err := json.NewDecoder(r.Body).Decode(&seen.Body); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
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
	return `{
	"data": ` + body + `
}`
}

// listing is a page of a repository's pullRequests connection holding the nodes.
func listing(hasNextPage bool, cursor string, nodes ...string) string {
	page := `{
	"pageInfo": {
		"hasNextPage": ` + boolOf(hasNextPage) + `,
		"endCursor": "` + cursor + `"
	},
	"nodes": [
` + strings.Join(nodes, ",\n") + `
	]
}`

	return data(`{
	"repository": {
		"pullRequests": ` + page + `
	}
}`)
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

// failingMergeReadiness is the merge readiness fields GitHub answers beside a pull request on a
// read of branches, the recorded shape with the values made up.
const failingMergeReadiness = `	"reviewDecision": "CHANGES_REQUESTED",
	"mergeStateStatus": "UNSTABLE",
	"autoMergeRequest": null,
	"viewerCanEnableAutoMerge": true`

// withMergeReadiness is a pull request node carrying the merge readiness fields.
func withMergeReadiness(node, readiness string) string {
	return strings.TrimSuffix(node, "\n}") + ",\n" + readiness + "\n}"
}

// branchesRead is GitHub answering a read of branches, one alias per branch.
func branchesRead(aliases ...string) string {
	fields := make([]string, len(aliases))
	for i, nodes := range aliases {
		fields[i] = `		"b` + strconv.Itoa(i) + `": {
			"nodes": [
` + nodes + `
			]
		}`
	}

	return data(`{
	"repository": {
` + strings.Join(fields, ",\n") + `
	}
}`)
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
			body: `{
	"data": {
		"repository": null
	},
	"errors": [
		{
			"type": "NOT_FOUND",
			"path": [
				"repository"
			],
			"locations": [
				{
					"line": 2,
					"column": 3
				}
			],
			"message": "Could not resolve to a Repository with the name 'harrisoncramer/no-such-repository-xyz'."
		}
	]
}`,
			call: func(f *Forge) error {
				return f.Merge(t.Context(), "https://github.com/harrisoncramer/no-such-repository-xyz/pull/1")
			},
		},
		{
			name: "a pull request github does not have",
			body: `{
	"data": {
		"repository": {
			"pullRequest": null
		}
	},
	"errors": [
		{
			"type": "NOT_FOUND",
			"path": [
				"repository",
				"pullRequest"
			],
			"locations": [
				{
					"line": 3,
					"column": 5
				}
			],
			"message": "Could not resolve to a PullRequest with the number of 999."
		}
	]
}`,
			call: func(f *Forge) error {
				return f.Merge(t.Context(), "https://github.com/harrisoncramer/komrad-forge-fixture/pull/999")
			},
		},
		{
			name: "a repository the listing cannot see",
			body: `{
	"data": {
		"repository": null
	},
	"errors": [
		{
			"type": "NOT_FOUND",
			"path": [
				"repository"
			],
			"locations": [
				{
					"line": 2,
					"column": 3
				}
			],
			"message": "Could not resolve to a Repository with the name 'harrisoncramer/no-such-repository-xyz'."
		}
	]
}`,
			call: func(f *Forge) error {
				_, err := f.ReadBranches(t.Context(), komrad, []string{"main"})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, auth, _ := serve(t, map[string]answer{"MergeTarget": {body: tt.body}, "ReadBranches": {body: tt.body}})
			require.ErrorIs(t, tt.call(f), forge.ErrNotFound)
			assert.Equal(t, 0, auth.invalidated)
		})
	}

	misrouted, _, _ := serve(t, nil)
	err := misrouted.Merge(t.Context(), "https://github.com/harrisoncramer/komrad/pull/1")
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrNotFound)
	assert.ErrorContains(t, err, "/graphql")
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
	assert.EqualError(t, err, refused.Error())
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
			name:   "a spent primary budget refused in the errors says when it resets",
			status: http.StatusOK,
			header: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1789900000"},
			body: `{
	"data": null,
	"errors": [
		{
			"type": "RATE_LIMITED",
			"message": "API rate limit exceeded for user ID 12345."
		}
	]
}`,
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

package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const openPull = `{
  "node_id": "PR_open",
  "html_url": "https://github.com/harrisoncramer/weave/pull/700",
  "number": 700,
  "title": "open one",
  "state": "open",
  "draft": true,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T11:00:00Z",
  "merged_at": null,
  "closed_at": null,
  "head": {"ref": "agent/x", "sha": "aaa", "repo": {"html_url": "https://github.com/harrisoncramer/weave"}},
  "base": {"ref": "main", "sha": "bbb", "repo": {"html_url": "https://github.com/harrisoncramer/weave"}}
}`

const mergedPull = `{
  "node_id": "PR_merged",
  "html_url": "https://github.com/harrisoncramer/weave/pull/680",
  "number": 680,
  "title": "merged one",
  "state": "closed",
  "draft": false,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T12:00:00Z",
  "merged_at": "2026-09-18T12:00:00Z",
  "closed_at": "2026-09-18T12:00:00Z",
  "head": {"ref": "agent/x", "sha": "ccc", "repo": {"html_url": "https://github.com/harrisoncramer/weave"}},
  "base": {"ref": "main", "sha": "ddd", "repo": {"html_url": "https://github.com/harrisoncramer/weave"}}
}`

const forkPull = `{
  "node_id": "PR_fork",
  "html_url": "https://github.com/harrisoncramer/weave/pull/701",
  "number": 701,
  "title": "from a fork",
  "state": "open",
  "draft": false,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T10:00:00Z",
  "merged_at": null,
  "closed_at": null,
  "head": {"ref": "fix", "sha": "eee", "repo": {"html_url": "https://github.com/someone/weave"}},
  "base": {"ref": "main", "sha": "fff", "repo": {"html_url": "https://github.com/harrisoncramer/weave"}}
}`

const deletedForkPull = `{
  "node_id": "PR_gone",
  "html_url": "https://github.com/harrisoncramer/weave/pull/702",
  "number": 702,
  "title": "fork deleted",
  "state": "closed",
  "draft": false,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T10:00:00Z",
  "merged_at": null,
  "closed_at": "2026-09-18T13:00:00Z",
  "head": {"ref": "fix", "sha": "ggg", "repo": null},
  "base": {"ref": "main", "sha": "hhh", "repo": {"html_url": "https://github.com/harrisoncramer/weave"}}
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

// serve is a GitHub that answers one path with one body, recording the request it saw.
func serve(t *testing.T, path, body string, status int) (*Forge, *fakeAuth, *http.Request) {
	t.Helper()

	var seen http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		if r.URL.Path != path {
			http.NotFound(w, r)

			return
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	auth := &fakeAuth{token: "secret"}

	return New(NewParams{
		Auth: auth,
		API:  server.URL,
		Next: server.Client().Transport,
	}), auth, &seen
}

var weave = git.RemoteURL{
	Raw:    "git@github.com:harrisoncramer/weave.git",
	Scheme: "ssh",
	Host:   "github.com",
	Path:   "harrisoncramer/weave",
}

// TestList covers the list call: the head filter GitHub wants, the token on the
// request, and every row mapped into the neutral record.
func TestList(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/repos/harrisoncramer/weave/pulls", "["+openPull+","+mergedPull+","+forkPull+","+deletedForkPull+"]", http.StatusOK)

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    weave,
		Branch:  "agent/x",
		Page:    2,
		PerPage: 50,
	})
	require.NoError(t, err)

	assert.Equal(t, "harrisoncramer:agent/x", seen.URL.Query().Get("head"))
	assert.Equal(t, "all", seen.URL.Query().Get("state"))
	assert.Equal(t, "2", seen.URL.Query().Get("page"))
	assert.Equal(t, "50", seen.URL.Query().Get("per_page"))
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))
	assert.False(t, listed.HasMore)
	pullRequests := listed.PullRequests
	require.Len(t, pullRequests, 4)

	stamp := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_open",
		URL:        "https://github.com/harrisoncramer/weave/pull/700",
		Repository: git.RemoteURL{
			Raw:    "https://github.com/harrisoncramer/weave",
			Scheme: "https",
			Host:   "github.com",
			Path:   "harrisoncramer/weave",
		},
		Head: git.RemoteURL{
			Raw:    "https://github.com/harrisoncramer/weave",
			Scheme: "https",
			Host:   "github.com",
			Path:   "harrisoncramer/weave",
		},
		Number:     700,
		Title:      "open one",
		HeadBranch: "agent/x",
		HeadSHA:    "aaa",
		BaseBranch: "main",
		BaseSHA:    "bbb",
		Draft:      true,
		State:      "open",
		CreatedAt:  time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC),
	}, pullRequests[0])
	assert.False(t, pullRequests[0].IsFinished())
	assert.False(t, pullRequests[0].IsCrossRepository())

	assert.Equal(t, "PR_merged", pullRequests[1].ProviderID)
	assert.Equal(t, &stamp, pullRequests[1].MergedAt)
	assert.Equal(t, &stamp, pullRequests[1].ClosedAt)
	assert.True(t, pullRequests[1].IsFinished())

	assert.Equal(t, "someone/weave", pullRequests[2].Head.Path)
	assert.True(t, pullRequests[2].IsCrossRepository())

	assert.True(t, pullRequests[3].Head.IsZero())
	assert.True(t, pullRequests[3].IsCrossRepository())
	assert.True(t, pullRequests[3].IsFinished())
}

// TestListWithoutABranch covers the repository's own listing: open ones only, no head filter.
func TestListWithoutABranch(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/repos/harrisoncramer/weave/pulls", "["+openPull+"]", http.StatusOK)

	listed, err := f.List(t.Context(), forge.ListParams{Repo: weave})
	require.NoError(t, err)
	require.Len(t, listed.PullRequests, 1)
	assert.Equal(t, "open", seen.URL.Query().Get("state"))
	assert.Equal(t, "1", seen.URL.Query().Get("page"))
	assert.False(t, seen.URL.Query().Has("head"))
}

// TestListReadsTheNextPageFromLink covers GitHub saying another page follows.
func TestListReadsTheNextPageFromLink(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://api.github.com/repos/harrisoncramer/weave/pulls?page=2>; rel="next", <https://api.github.com/repos/harrisoncramer/weave/pulls?page=9>; rel="last"`)
		_, _ = w.Write([]byte("[" + openPull + "]"))
	}))
	t.Cleanup(server.Close)

	f := New(NewParams{
		Auth: &fakeAuth{token: "secret"},
		API:  server.URL,
		Next: server.Client().Transport,
	})
	listed, err := f.List(t.Context(), forge.ListParams{Repo: weave})
	require.NoError(t, err)
	assert.True(t, listed.HasMore)
}

// TestGet covers reading one pull request by its web address.
func TestGet(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/repos/harrisoncramer/weave/pulls/680", mergedPull, http.StatusOK)

	pullRequest, err := f.Get(t.Context(), "https://github.com/harrisoncramer/weave/pull/680")
	require.NoError(t, err)
	assert.Equal(t, 680, pullRequest.Number)
	assert.Equal(t, "merged one", pullRequest.Title)
}

// TestGetRefusesWhatIsNotAPullRequest covers the addresses Get will not ask GitHub about.
func TestGetRefusesWhatIsNotAPullRequest(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/never", "", http.StatusOK)

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
		})
	}
}

// TestListRefusesANestedPath covers a repository path GitHub cannot address.
func TestListRefusesANestedPath(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/never", "", http.StatusOK)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
}

// TestUnauthorizedInvalidatesTheCredential covers a refused token: GitHub's 401 reaches the
// Authenticator so the connection reads as unverified.
func TestUnauthorizedInvalidatesTheCredential(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, "/repos/harrisoncramer/weave/pulls", `{"message":"Bad credentials"}`, http.StatusUnauthorized)

	_, err := f.List(t.Context(), forge.ListParams{Repo: weave, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestVerify covers asking GitHub who the credential is, and a token GitHub refuses.
func TestVerify(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/user", `{"login":"harrisoncramer"}`, http.StatusOK)

	login, err := f.Verify(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))

	refused, auth, _ := serve(t, "/user", `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	_, err = refused.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestNotFound covers a repository or pull request GitHub does not have, or will not show.
func TestNotFound(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, "/never", "", http.StatusOK)

	_, err := f.Get(t.Context(), "https://github.com/harrisoncramer/weave/pull/1")
	require.ErrorIs(t, err, forge.ErrNotFound)
	assert.Equal(t, 0, auth.invalidated)
}

// TestVerifyInstallation covers what an installation token can be asked: how many repositories
// it reaches, and a refusal reaching the Authenticator.
func TestVerifyInstallation(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/installation/repositories", `{"total_count":4,"repositories":[{"full_name":"harrisoncramer/weave"}]}`, http.StatusOK)

	reached, err := f.VerifyInstallation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, reached)
	assert.Equal(t, "1", seen.URL.Query().Get("per_page"))

	refused, auth, _ := serve(t, "/installation/repositories", `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	_, err = refused.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
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
	f := New(NewParams{Auth: refusingAuth{err: refused}, API: "http://127.0.0.1:1"})

	_, err := f.VerifyInstallation(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, refused.Error(), err.Error())
}

// listUpdatedServer answers the sync's listing with whatever the handler writes.
func listUpdatedServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *Forge {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(handle))
	t.Cleanup(server.Close)

	f := New(NewParams{
		Auth: &fakeAuth{token: "secret"},
		API:  server.URL,
		Next: server.Client().Transport,
		Now: func() time.Time {
			return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
		},
	})

	return f
}

// TestListUpdated covers the sync's request: every state by last change, the largest page,
// the ETag carried in and out, the rate limit read off the headers, and the next page from
// Link.
func TestListUpdated(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	f := listUpdatedServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.Header().Set("ETag", `W/"abc"`)
		w.Header().Set("Link", `<https://api.github.com/repos/harrisoncramer/weave/pulls?page=3>; rel="next"`)
		w.Header().Set("X-RateLimit-Remaining", "4321")
		w.Header().Set("X-RateLimit-Reset", "1789900000")
		_, _ = w.Write([]byte("[" + openPull + "," + mergedPull + "]"))
	})

	listed, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{
		Repo: weave,
		Page: 2,
		ETag: `W/"old"`,
	})
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.Equal(t, "/repos/harrisoncramer/weave/pulls", seen.URL.Path)
	assert.Equal(t, "all", seen.URL.Query().Get("state"))
	assert.Equal(t, "updated", seen.URL.Query().Get("sort"))
	assert.Equal(t, "desc", seen.URL.Query().Get("direction"))
	assert.Equal(t, "100", seen.URL.Query().Get("per_page"))
	assert.Equal(t, "2", seen.URL.Query().Get("page"))
	assert.Equal(t, `W/"old"`, seen.Header.Get("If-None-Match"))
	assert.False(t, listed.NotModified)
	assert.Len(t, listed.PullRequests, 2)
	assert.True(t, listed.HasMore)
	assert.Equal(t, `W/"abc"`, listed.ETag)
	assert.True(t, listed.RateLimit.Known)
	assert.Equal(t, 4321, listed.RateLimit.Remaining)
	assert.Equal(t, time.Unix(1789900000, 0).UTC(), listed.RateLimit.ResetAt.UTC())
}

// TestListUpdatedNotModified covers a 304, which comes back as such with no rows and no error,
// and a request with no ETag sending no condition.
func TestListUpdatedNotModified(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	f := listUpdatedServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		w.Header().Set("ETag", `W/"abc"`)
		w.WriteHeader(http.StatusNotModified)
	})

	listed, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{
		Repo: weave,
		Page: 1,
		ETag: `W/"abc"`,
	})
	require.NoError(t, err)
	assert.True(t, listed.NotModified)
	assert.Empty(t, listed.PullRequests)
	assert.False(t, listed.HasMore)
	assert.Equal(t, `W/"abc"`, listed.ETag)

	f = listUpdatedServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r
		_, _ = w.Write([]byte("[]"))
	})
	_, err = f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: weave, Page: 1})
	require.NoError(t, err)
	assert.Empty(t, seen.Header.Values("If-None-Match"))
}

// TestRateLimitRefusals covers the three ways GitHub says to wait and the one 403 that is not
// a rate limit at all.
func TestRateLimitRefusals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		status      int
		header      map[string]string
		body        string
		wantThrottl bool
		wantRetryAt time.Time
	}{
		{
			name:        "a bare 403 is a permission answer",
			status:      http.StatusForbidden,
			body:        `{"message":"Resource not accessible by integration"}`,
			wantThrottl: false,
		},
		{
			name:        "retry-after says how long",
			status:      http.StatusForbidden,
			header:      map[string]string{"Retry-After": "30"},
			body:        `{"message":"You have exceeded a secondary rate limit"}`,
			wantThrottl: true,
			wantRetryAt: now.Add(30 * time.Second),
		},
		{
			name:        "a spent primary budget says when it resets",
			status:      http.StatusForbidden,
			header:      map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1789900000"},
			body:        `{"message":"API rate limit exceeded"}`,
			wantThrottl: true,
			wantRetryAt: time.Unix(1789900000, 0),
		},
		{
			name:        "a 429 with nothing else is a refusal with no time",
			status:      http.StatusTooManyRequests,
			body:        `{"message":"slow down"}`,
			wantThrottl: true,
		},
		{
			name:        "the secondary limit body with no header is a refusal with no time",
			status:      http.StatusForbidden,
			body:        `{"message":"You have triggered an abuse detection mechanism"}`,
			wantThrottl: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := listUpdatedServer(t, func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.header {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			_, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: weave, Page: 1})
			require.Error(t, err)

			var throttled *forge.ThrottledError
			if !tt.wantThrottl {
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

// TestGetReviewDecision covers the one GraphQL call: the query and its variables as GitHub
// wants them, every decision GitHub answers, a null decision, a pull request GitHub does not
// have, and the refusals the REST calls already turn into weave's.
func TestGetReviewDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		want    forge.ReviewDecision
		wantErr error
	}{
		{
			name:    "a refused credential",
			status:  http.StatusUnauthorized,
			body:    `{"message":"Bad credentials"}`,
			wantErr: forge.ErrUnauthorized,
		},
		{
			name:    "a spent rate limit",
			status:  http.StatusTooManyRequests,
			body:    `{"message":"slow down"}`,
			wantErr: forge.ErrThrottled,
		},
		{
			name:    "a pull request github does not have answers in the errors list",
			status:  http.StatusOK,
			body:    `{"data":{"repository":{"pullRequest":null}},"errors":[{"type":"NOT_FOUND","path":["repository","pullRequest"],"message":"Could not resolve to a PullRequest with the number of 999."}]}`,
			wantErr: forge.ErrNotFound,
		},
		{
			name:    "a repository github does not have answers null",
			status:  http.StatusOK,
			body:    `{"data":{"repository":null}}`,
			wantErr: forge.ErrNotFound,
		},
		{
			name:   "approved",
			status: http.StatusOK,
			body:   `{"data":{"repository":{"pullRequest":{"reviewDecision":"APPROVED"}}}}`,
			want:   forge.ReviewDecisionApproved,
		},
		{
			name:   "changes requested",
			status: http.StatusOK,
			body:   `{"data":{"repository":{"pullRequest":{"reviewDecision":"CHANGES_REQUESTED"}}}}`,
			want:   forge.ReviewDecisionChangesRequested,
		},
		{
			name:   "review required is pending",
			status: http.StatusOK,
			body:   `{"data":{"repository":{"pullRequest":{"reviewDecision":"REVIEW_REQUIRED"}}}}`,
			want:   forge.ReviewDecisionPending,
		},
		{
			name:   "nothing said is none",
			status: http.StatusOK,
			body:   `{"data":{"repository":{"pullRequest":{"reviewDecision":null}}}}`,
			want:   forge.ReviewDecisionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seen *http.Request
			var sent map[string]any
			f := listUpdatedServer(t, func(w http.ResponseWriter, r *http.Request) {
				seen = r
				_ = json.NewDecoder(r.Body).Decode(&sent)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			got, err := f.GetReviewDecisionByNumber(t.Context(), weave, 740)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			require.NotNil(t, seen)
			assert.Equal(t, http.MethodPost, seen.Method)
			assert.Equal(t, "/graphql", seen.URL.Path)
			assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))
			assert.Equal(t, reviewDecisionQuery, sent["query"])
			assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "weave", "number": float64(740)}, sent["variables"])
		})
	}
}

// TestGetReviewDecisionTransport404IsNotTheForgeSayingNotFound covers a host whose GraphQL is
// somewhere else, which is a misrouted call rather than a pull request GitHub does not have.
func TestGetReviewDecisionTransport404IsNotTheForgeSayingNotFound(t *testing.T) {
	t.Parallel()

	f := listUpdatedServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	_, err := f.GetReviewDecisionByNumber(t.Context(), weave, 740)
	require.Error(t, err)
	require.NotErrorIs(t, err, forge.ErrNotFound)
	assert.Contains(t, err.Error(), "/graphql")
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

			f := New(NewParams{Auth: &fakeAuth{}, API: tt.api})
			assert.Equal(t, tt.want, f.graphqlURL())
		})
	}
}

// TestGetReviewDecisionRefusesANestedPath covers a repository path GitHub cannot address, which
// costs no request.
func TestGetReviewDecisionRefusesANestedPath(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/never", "", http.StatusOK)

	_, err := f.GetReviewDecisionByNumber(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, 1)
	require.ErrorIs(t, err, forge.ErrNotRepository)
}

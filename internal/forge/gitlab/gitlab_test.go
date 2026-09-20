package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const openMergeRequest = `{
  "id": 9001,
  "iid": 7,
  "web_url": "https://gitlab.com/harrisoncramer/weave/-/merge_requests/7",
  "title": "open one",
  "state": "opened",
  "draft": true,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T11:00:00Z",
  "merged_at": null,
  "closed_at": null,
  "source_branch": "agent/x",
  "target_branch": "main",
  "sha": "aaa",
  "source_project_id": 42,
  "target_project_id": 42,
  "diff_refs": {"base_sha": "bbb", "head_sha": "aaa", "start_sha": "bbb"}
}`

const mergedMergeRequest = `{
  "id": 9002,
  "iid": 6,
  "web_url": "https://gitlab.com/harrisoncramer/weave/-/merge_requests/6",
  "title": "merged one",
  "state": "merged",
  "draft": false,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T12:00:00Z",
  "merged_at": "2026-09-18T12:00:00Z",
  "closed_at": null,
  "source_branch": "agent/x",
  "target_branch": "main",
  "sha": "ccc",
  "source_project_id": 42,
  "target_project_id": 42
}`

const forkMergeRequest = `{
  "id": 9003,
  "iid": 8,
  "web_url": "https://gitlab.com/harrisoncramer/weave/-/merge_requests/8",
  "title": "from a fork",
  "state": "opened",
  "draft": false,
  "created_at": "2026-09-18T10:00:00Z",
  "updated_at": "2026-09-18T10:00:00Z",
  "merged_at": null,
  "closed_at": null,
  "source_branch": "fix",
  "target_branch": "main",
  "sha": "eee",
  "source_project_id": 77,
  "target_project_id": 42
}`

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

// serve is a GitLab that answers one path with one body, recording the request it saw.
func serve(t *testing.T, path, body string, status int) (*Forge, *fakeAuth, *http.Request) {
	t.Helper()

	return serveWith(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != path {
			http.NotFound(w, r)

			return
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// serveWith is a GitLab answering however the handler says, recording the last request.
func serveWith(t *testing.T, handle http.HandlerFunc) (*Forge, *fakeAuth, *http.Request) {
	t.Helper()

	var seen http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		handle(w, r)
	}))
	t.Cleanup(server.Close)

	auth := &fakeAuth{token: "secret"}

	return New(NewParams{
		Auth: auth,
		API:  server.URL,
		Next: server.Client().Transport,
		Now: func() time.Time {
			return time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
		},
	}), auth, &seen
}

var weave = git.RemoteURL{
	Raw:    "git@gitlab.com:harrisoncramer/weave.git",
	Scheme: "ssh",
	Host:   "gitlab.com",
	Path:   "harrisoncramer/weave",
}

// TestList covers the list call: the project addressed as one escaped segment, the source
// branch filter, the token in GitLab's own header, and every row mapped into the neutral
// record with a fork's head left unknown.
func TestList(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/projects/harrisoncramer%2Fweave/merge_requests", "["+openMergeRequest+","+mergedMergeRequest+","+forkMergeRequest+"]", http.StatusOK)

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    weave,
		Branch:  "agent/x",
		Page:    2,
		PerPage: 50,
	})
	require.NoError(t, err)

	assert.Equal(t, "agent/x", seen.URL.Query().Get("source_branch"))
	assert.Equal(t, "all", seen.URL.Query().Get("state"))
	assert.Equal(t, "2", seen.URL.Query().Get("page"))
	assert.Equal(t, "50", seen.URL.Query().Get("per_page"))
	assert.Equal(t, "secret", seen.Header.Get("PRIVATE-TOKEN"))
	assert.False(t, listed.HasMore)
	pullRequests := listed.PullRequests
	require.Len(t, pullRequests, 3)

	repository := git.RemoteURL{
		Raw:    "https://gitlab.com/harrisoncramer/weave",
		Scheme: "https",
		Host:   "gitlab.com",
		Path:   "harrisoncramer/weave",
	}
	assert.Equal(t, forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: "9001",
		URL:        "https://gitlab.com/harrisoncramer/weave/-/merge_requests/7",
		Repository: repository,
		Head:       repository,
		Number:     7,
		Title:      "open one",
		HeadBranch: "agent/x",
		HeadSHA:    "aaa",
		BaseBranch: "main",
		BaseSHA:    "bbb",
		Draft:      true,
		State:      "opened",
		CreatedAt:  time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC),
	}, pullRequests[0])
	assert.False(t, pullRequests[0].IsCrossRepository())

	stamp := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, "9002", pullRequests[1].ProviderID)
	assert.Equal(t, &stamp, pullRequests[1].MergedAt)
	assert.Equal(t, &stamp, pullRequests[1].ClosedAt, "a merge closes the merge request too")
	assert.Empty(t, pullRequests[1].BaseSHA, "a listing without diff refs names no base")
	assert.True(t, pullRequests[1].IsFinished())

	assert.True(t, pullRequests[2].Head.IsZero(), "a fork's project is not in the payload")
	assert.True(t, pullRequests[2].IsCrossRepository())
}

// TestListWithoutABranch covers the project's own listing: open ones only, no branch filter.
func TestListWithoutABranch(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/projects/harrisoncramer%2Fweave/merge_requests", "["+openMergeRequest+"]", http.StatusOK)

	_, err := f.List(t.Context(), forge.ListParams{Repo: weave, PerPage: 20})
	require.NoError(t, err)

	assert.Equal(t, "opened", seen.URL.Query().Get("state"))
	assert.Empty(t, seen.URL.Query().Get("source_branch"))
}

// TestListReadsTheNextPageFromTheHeader covers GitLab naming the page after this one.
func TestListReadsTheNextPageFromTheHeader(t *testing.T) {
	t.Parallel()

	f, _, _ := serveWith(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Next-Page", "3")
		_, _ = w.Write([]byte("[" + openMergeRequest + "]"))
	})

	listed, err := f.List(t.Context(), forge.ListParams{
		Repo:    weave,
		Branch:  "agent/x",
		Page:    2,
		PerPage: 1,
	})
	require.NoError(t, err)
	assert.True(t, listed.HasMore)
}

// TestListAddressesANestedGroup covers a project under a subgroup, which is one escaped path
// segment to GitLab.
func TestListAddressesANestedGroup(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/projects/group%2Fsub%2Frepo/merge_requests", "[]", http.StatusOK)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "gitlab.com", Path: "group/sub/repo"}, Branch: "main"})
	require.NoError(t, err)
	assert.Equal(t, "/projects/group%2Fsub%2Frepo/merge_requests", seen.URL.EscapedPath())
}

// TestListUpdated covers the sync's request: every state by last change, the largest page,
// the ETag carried in and out, and the rate limit read off GitLab's headers.
func TestListUpdated(t *testing.T) {
	t.Parallel()

	f, _, seen := serveWith(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `W/"abc"`)
		w.Header().Set("X-Next-Page", "3")
		w.Header().Set("RateLimit-Remaining", "1999")
		w.Header().Set("RateLimit-Reset", "1789900000")
		_, _ = w.Write([]byte("[" + openMergeRequest + "," + mergedMergeRequest + "]"))
	})

	listed, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{
		Repo: weave,
		Page: 2,
		ETag: `W/"old"`,
	})
	require.NoError(t, err)
	assert.Equal(t, "all", seen.URL.Query().Get("state"))
	assert.Equal(t, "updated_at", seen.URL.Query().Get("order_by"))
	assert.Equal(t, "desc", seen.URL.Query().Get("sort"))
	assert.Equal(t, "100", seen.URL.Query().Get("per_page"))
	assert.Equal(t, "2", seen.URL.Query().Get("page"))
	assert.Equal(t, `W/"old"`, seen.Header.Get("If-None-Match"))
	assert.False(t, listed.NotModified)
	assert.Len(t, listed.PullRequests, 2)
	assert.True(t, listed.HasMore)
	assert.Equal(t, `W/"abc"`, listed.ETag)
	assert.True(t, listed.RateLimit.Known)
	assert.Equal(t, 1999, listed.RateLimit.Remaining)
	assert.Equal(t, time.Unix(1789900000, 0).UTC(), listed.RateLimit.ResetAt.UTC())
}

// TestListUpdatedNotModified covers a 304, which comes back as such with no rows and no error.
func TestListUpdatedNotModified(t *testing.T) {
	t.Parallel()

	f, _, _ := serveWith(t, func(w http.ResponseWriter, _ *http.Request) {
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
	assert.Equal(t, `W/"abc"`, listed.ETag)
}

// TestGet covers one merge request by its web address.
func TestGet(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/projects/harrisoncramer%2Fweave/merge_requests/6", mergedMergeRequest, http.StatusOK)

	pullRequest, err := f.Get(t.Context(), "https://gitlab.com/harrisoncramer/weave/-/merge_requests/6")
	require.NoError(t, err)
	assert.Equal(t, 6, pullRequest.Number)
	assert.Equal(t, "merged one", pullRequest.Title)
}

// TestGetRefusesWhatIsNotAMergeRequest covers the addresses Get will not ask GitLab about.
func TestGetRefusesWhatIsNotAMergeRequest(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/never", "", http.StatusOK)

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
		})
	}
}

// TestListRefusesABarePath covers a repository path GitLab cannot address.
func TestListRefusesABarePath(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/never", "", http.StatusOK)

	_, err := f.List(t.Context(), forge.ListParams{Repo: git.RemoteURL{Host: "gitlab.com", Path: "repo"}, Branch: "main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
}

// TestVerify covers asking GitLab who the credential is, and a token GitLab refuses reaching
// the Authenticator.
func TestVerify(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/user", `{"id":1,"username":"harrisoncramer"}`, http.StatusOK)

	login, err := f.Verify(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer", login)

	refused, auth, _ := serve(t, "/user", `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	_, err = refused.Verify(t.Context())
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

// TestNotFound covers a project or merge request GitLab does not have, or will not show.
func TestNotFound(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, "/never", "", http.StatusOK)

	_, err := f.Get(t.Context(), "https://gitlab.com/harrisoncramer/weave/-/merge_requests/1")
	require.ErrorIs(t, err, forge.ErrNotFound)
	assert.Equal(t, 0, auth.invalidated)
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

			f, _, _ := serveWith(t, func(w http.ResponseWriter, _ *http.Request) {
				for key, value := range tt.header {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"refused"}`))
			})

			_, err := f.ListUpdated(t.Context(), forge.ListUpdatedParams{Repo: weave, Page: 1})
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

// TestGetReviewDecisionByNumber covers the two reads behind a decision: a reviewer asking for
// changes settles it, then the approvals say whether the rules are met or still owed.
func TestGetReviewDecisionByNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reviewers string
		approvals string
		want      forge.ReviewDecision
		wantErr   error
	}{
		{
			name:      "a reviewer asking for changes",
			reviewers: `[{"user":{"id":1},"state":"requested_changes"},{"user":{"id":2},"state":"approved"}]`,
			approvals: `{"approvals_required":1,"approvals_left":0,"approved_by":[{"user":{"id":2}}]}`,
			want:      forge.ReviewDecisionChangesRequested,
		},
		{
			name:      "approvals still owed",
			reviewers: `[{"user":{"id":1},"state":"unreviewed"}]`,
			approvals: `{"approvals_required":2,"approvals_left":1,"approved_by":[{"user":{"id":2}}]}`,
			want:      forge.ReviewDecisionPending,
		},
		{
			name:      "approved",
			reviewers: `[]`,
			approvals: `{"approvals_required":1,"approvals_left":0,"approved_by":[{"user":{"id":2}}]}`,
			want:      forge.ReviewDecisionApproved,
		},
		{
			name:      "no rule and nobody has said anything",
			reviewers: `[]`,
			approvals: `{"approvals_required":0,"approvals_left":0,"approved_by":[]}`,
			want:      forge.ReviewDecisionNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var paths []string
			f, _, _ := serveWith(t, func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.EscapedPath())
				switch {
				case r.URL.Path == "/projects/harrisoncramer/weave/merge_requests/7/reviewers":
					_, _ = w.Write([]byte(tt.reviewers))
				case r.URL.Path == "/projects/harrisoncramer/weave/merge_requests/7/approvals":
					_, _ = w.Write([]byte(tt.approvals))
				default:
					http.NotFound(w, r)
				}
			})

			got, err := f.GetReviewDecisionByNumber(t.Context(), weave, 7)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, "/projects/harrisoncramer%2Fweave/merge_requests/7/reviewers", paths[0])
		})
	}
}

// TestGetReviewDecisionByNumberRefusals covers a merge request GitLab does not have and a
// refused credential.
func TestGetReviewDecisionByNumberRefusals(t *testing.T) {
	t.Parallel()

	missing, _, _ := serve(t, "/never", "", http.StatusOK)
	_, err := missing.GetReviewDecisionByNumber(t.Context(), weave, 7)
	require.ErrorIs(t, err, forge.ErrNotFound)

	refused, auth, _ := serve(t, "/projects/harrisoncramer%2Fweave/merge_requests/7/reviewers", `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	_, err = refused.GetReviewDecisionByNumber(t.Context(), weave, 7)
	require.ErrorIs(t, err, forge.ErrUnauthorized)
	assert.Equal(t, 1, auth.invalidated)
}

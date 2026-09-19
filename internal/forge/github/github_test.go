package github

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

// TestListOnBranch covers the list call: the head filter GitHub wants, the token on the
// request, and every row mapped into the neutral record.
func TestListOnBranch(t *testing.T) {
	t.Parallel()

	f, _, seen := serve(t, "/repos/harrisoncramer/weave/pulls", "["+openPull+","+mergedPull+","+forkPull+","+deletedForkPull+"]", http.StatusOK)

	pulls, err := f.ListOnBranch(t.Context(), weave, "agent/x")
	require.NoError(t, err)

	assert.Equal(t, "harrisoncramer:agent/x", seen.URL.Query().Get("head"))
	assert.Equal(t, "all", seen.URL.Query().Get("state"))
	assert.Equal(t, "Bearer secret", seen.Header.Get("Authorization"))
	require.Len(t, pulls, 4)

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
	}, pulls[0])
	assert.False(t, pulls[0].IsFinished())
	assert.False(t, pulls[0].IsCrossRepository())

	assert.Equal(t, "PR_merged", pulls[1].ProviderID)
	assert.Equal(t, &stamp, pulls[1].MergedAt)
	assert.Equal(t, &stamp, pulls[1].ClosedAt)
	assert.True(t, pulls[1].IsFinished())

	assert.Equal(t, "someone/weave", pulls[2].Head.Path)
	assert.True(t, pulls[2].IsCrossRepository())

	assert.True(t, pulls[3].Head.IsZero())
	assert.True(t, pulls[3].IsCrossRepository())
	assert.True(t, pulls[3].IsFinished())
}

// TestGet covers reading one pull request by its web address.
func TestGet(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/repos/harrisoncramer/weave/pulls/680", mergedPull, http.StatusOK)

	pull, err := f.Get(t.Context(), "https://github.com/harrisoncramer/weave/pull/680")
	require.NoError(t, err)
	assert.Equal(t, 680, pull.Number)
	assert.Equal(t, "merged one", pull.Title)
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

// TestListOnBranchRefusesANestedPath covers a repository path GitHub cannot address.
func TestListOnBranchRefusesANestedPath(t *testing.T) {
	t.Parallel()

	f, _, _ := serve(t, "/never", "", http.StatusOK)

	_, err := f.ListOnBranch(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, "main")
	require.ErrorIs(t, err, forge.ErrNotRepository)
}

// TestUnauthorizedInvalidatesTheCredential covers a refused token: GitHub's 401 reaches the
// Authenticator so the connection reads as unverified.
func TestUnauthorizedInvalidatesTheCredential(t *testing.T) {
	t.Parallel()

	f, auth, _ := serve(t, "/repos/harrisoncramer/weave/pulls", `{"message":"Bad credentials"}`, http.StatusUnauthorized)

	_, err := f.ListOnBranch(t.Context(), weave, "main")
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

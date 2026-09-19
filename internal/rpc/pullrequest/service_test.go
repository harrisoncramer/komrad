package pullrequest

import (
	"context"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeForge struct {
	pullRequests []forge.PullRequest
	repo         git.RemoteURL
	branch       string
}

func (f *fakeForge) Kind() string                           { return forge.KindGitHub }
func (f *fakeForge) Verify(context.Context) (string, error) { return "", nil }
func (f *fakeForge) Get(context.Context, string) (forge.PullRequest, error) {
	return forge.PullRequest{}, nil
}

func (f *fakeForge) List(_ context.Context, repo git.RemoteURL, branch string) ([]forge.PullRequest, error) {
	f.repo, f.branch = repo, branch

	return f.pullRequests, nil
}

type fakeFinder struct {
	forge *fakeForge
}

func (f *fakeFinder) ConnectionFor(_ context.Context, host string) (*forgestore.Connection, error) {
	if host != "github.com" {
		return nil, store.ErrNotFound.Saying("%s is not connected", host)
	}

	return &forgestore.Connection{Host: host, Kind: forge.KindGitHub}, nil
}

func (f *fakeFinder) ForConnection(forgestore.Connection) (forge.Forge, error) { return f.forge, nil }

// TestListPullRequests covers the remote picking the connection by host, the branch reaching
// the forge, and each pull request mapped through with its repositories canonical.
func TestListPullRequests(t *testing.T) {
	t.Parallel()

	merged := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	upstream := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave",
	}
	fake := &fakeForge{pullRequests: []forge.PullRequest{{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_1",
		URL:        "https://github.com/harrisoncramer/weave/pull/1",
		Repository: upstream,
		Number:     1,
		Title:      "one",
		HeadBranch: "agent/x",
		MergedAt:   &merged,
		ClosedAt:   &merged,
	}}}
	service := NewPullRequestService(&fakeFinder{forge: fake})

	resp, failure := service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/weave.git",
		Branch:    "agent/x",
	})
	require.Nil(t, failure)
	assert.Equal(t, "harrisoncramer/weave", fake.repo.Path)
	assert.Equal(t, "agent/x", fake.branch)
	require.Len(t, resp.GetPullRequests(), 1)

	pullRequest := resp.GetPullRequests()[0]
	assert.Equal(t, "https://github.com/harrisoncramer/weave", pullRequest.GetRepositoryUrl())
	assert.Empty(t, pullRequest.GetHeadRepositoryUrl(), "a head the forge no longer has is empty rather than https:///")
	assert.NotNil(t, pullRequest.MergedAt)
}

// TestListPullRequestsRefusals covers a remote that is not one, and a host nothing is
// connected to.
func TestListPullRequestsRefusals(t *testing.T) {
	t.Parallel()

	service := NewPullRequestService(&fakeFinder{forge: &fakeForge{}})

	_, failure := service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RemoteUrl: "/local/path", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, git.ErrNotForgeRemote)

	_, failure = service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RemoteUrl: "git@gitlab.com:o/r.git", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, store.ErrNotFound)
}

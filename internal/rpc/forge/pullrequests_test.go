package forge

import (
	"context"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	forgestore "weave/internal/store/forge"
	"weave/lib/xptr"
	forgev1 "weave/proto/gen/forge/v1"
	forgev1weave "weave/proto/gen/forge/v1/forgev1weave"
	paginationv1 "weave/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

// fakeForge answers List from its fields and records each listing.
type fakeForge struct {
	forge.Forge
	pullRequests []forge.PullRequest
	hasMore      bool
	listed       []forge.ListParams
}

// List records the params and answers one page of pullRequests.
func (f *fakeForge) List(_ context.Context, params forge.ListParams) (*forge.Page, error) {
	f.listed = append(f.listed, params)

	return &forge.Page{
		PullRequests: f.pullRequests,
		HasMore:      f.hasMore,
		Cursor:       "end-of-page",
	}, nil
}

// TestListForgePullRequests pins what reaches the forge and how each pull request renders.
func TestListForgePullRequests(t *testing.T) {
	merged := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	listing := &fakeForge{pullRequests: []forge.PullRequest{{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_1",
		URL:        "https://github.com/harrisoncramer/weave/pull/1",
		Repository: git.RemoteURL{
			Scheme: "https",
			Host:   "github.com",
			Path:   "harrisoncramer/weave",
		},
		Number:     1,
		Title:      "one",
		HeadBranch: "agent/x",
		CreatedAt:  merged,
		UpdatedAt:  merged,
		MergedAt:   &merged,
	}}}
	connector := &fakeConnector{
		connections: []forgestore.Connection{connected()},
		forge:       listing,
	}
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(connector)}

	resp, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/weave.git",
		Branch:    "agent/x",
	}))
	require.NoError(t, err)

	want := &forgev1.ListForgePullRequestsResponse{PullRequests: []*forgev1.ForgePullRequest{{
		Forge:         forge.KindGitHub,
		ProviderId:    "PR_1",
		Url:           "https://github.com/harrisoncramer/weave/pull/1",
		RepositoryUrl: "https://github.com/harrisoncramer/weave",
		Number:        1,
		Title:         "one",
		HeadBranch:    "agent/x",
		CreatedAt:     "2026-09-18T12:00:00Z",
		UpdatedAt:     "2026-09-18T12:00:00Z",
		MergedAt:      xptr.Ptr("2026-09-18T12:00:00Z"),
	}}}
	assert.Equal(t, []string{"github.com"}, connector.hosts)
	require.Len(t, listing.listed, 1)
	assert.Equal(t, "harrisoncramer/weave", listing.listed[0].Repo.Path)
	assert.Equal(t, "agent/x", listing.listed[0].Branch)
	assert.Empty(t, listing.listed[0].Cursor)
	assert.Equal(t, 20, listing.listed[0].PerPage)
	assert.Empty(t, cmp.Diff(want, resp.Msg, protocmp.Transform()))
}

// TestListForgePullRequestsPages pins that a page token carries the forge's cursor and the page size.
func TestListForgePullRequestsPages(t *testing.T) {
	listing := &fakeForge{hasMore: true}
	connector := &fakeConnector{
		connections: []forgestore.Connection{connected()},
		forge:       listing,
	}
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(connector)}

	first, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr(int32(50))},
	}))
	require.NoError(t, err)
	require.NotNil(t, first.Msg.NextPageToken)

	listing.hasMore = false
	second, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)

	require.Len(t, listing.listed, 2)
	assert.Equal(t, 50, listing.listed[0].PerPage)
	assert.Equal(t, "end-of-page", listing.listed[1].Cursor)
	assert.Equal(t, 50, listing.listed[1].PerPage)
	assert.Nil(t, second.Msg.NextPageToken)
}

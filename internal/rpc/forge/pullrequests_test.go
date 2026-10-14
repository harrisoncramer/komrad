package forge

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"
	forgev1komrad "komrad/proto/gen/forge/v1/forgev1komrad"
	paginationv1 "komrad/proto/gen/pagination/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListForgePullRequestsPages pins that a page token carries the forge's cursor and the page size.
func TestListForgePullRequestsPages(t *testing.T) {
	connector := &fakeConnector{page: forge.Page{
		HasMore: true,
		Cursor:  "end-of-page",
	}}
	service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(connector, nil)}

	first, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/komrad.git",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr(int32(50))},
	}))
	require.NoError(t, err)
	require.NotNil(t, first.Msg.NextPageToken)

	connector.page.HasMore = false
	second, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/komrad.git",
		Pagination: &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)

	require.Len(t, connector.listed, 2)
	assert.Equal(t, 50, connector.listed[0].PerPage)
	assert.Equal(t, "end-of-page", connector.listed[1].Cursor)
	assert.Equal(t, 50, connector.listed[1].PerPage)
	assert.Nil(t, second.Msg.NextPageToken)
}

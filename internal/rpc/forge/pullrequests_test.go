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

// TestPlanForgePullRequestMerge pins each option, the default method and the head reaching the
// caller.
func TestPlanForgePullRequestMerge(t *testing.T) {
	writer := &fakeWriter{plan: forge.MergePlan{
		Options: []forge.MergeOption{
			{Method: forgev1.MergeMethod_MERGE_METHOD_MERGE, Message: &forge.CommitMessage{Headline: "Merge pull request #1", Body: "Land the fix"}},
			{Method: forgev1.MergeMethod_MERGE_METHOD_REBASE, Allowed: true},
		},
		DefaultMethod: forgev1.MergeMethod_MERGE_METHOD_REBASE,
		HeadSHA:       "abc123",
	}}
	service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(&fakeConnector{}, writer)}

	planned, err := service.PlanForgePullRequestMerge(t.Context(), connect.NewRequest(&forgev1.PlanForgePullRequestMergeRequest{
		Url: "https://github.com/harrisoncramer/komrad/pull/1",
	}))
	require.NoError(t, err)

	assert.Equal(t, forgev1.MergeMethod_MERGE_METHOD_REBASE, planned.Msg.GetDefaultMethod())
	assert.Equal(t, "abc123", planned.Msg.GetHeadSha())
	require.Len(t, planned.Msg.GetOptions(), 2)
	assert.Equal(t, forgev1.MergeMethod_MERGE_METHOD_MERGE, planned.Msg.GetOptions()[0].GetMethod())
	assert.False(t, planned.Msg.GetOptions()[0].GetAllowed())
	assert.Equal(t, "Merge pull request #1", planned.Msg.GetOptions()[0].GetMessage().GetHeadline())
	assert.Equal(t, "Land the fix", planned.Msg.GetOptions()[0].GetMessage().GetBody())
	assert.True(t, planned.Msg.GetOptions()[1].GetAllowed())
	assert.Nil(t, planned.Msg.GetOptions()[1].Message)
}

// TestMergeForgePullRequest pins the method, message and head reaching the writer, and a message
// without a method refused.
func TestMergeForgePullRequest(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"

	tests := []struct {
		name       string
		req        *forgev1.MergeForgePullRequestRequest
		wantCode   connect.Code
		wantMerged []forge.MergeParams
	}{
		{
			name:       "no method or message leaves both to the forge",
			req:        &forgev1.MergeForgePullRequestRequest{Url: address},
			wantMerged: []forge.MergeParams{{URL: address}},
		},
		{
			name: "a method and message reach the forge with the head",
			req: &forgev1.MergeForgePullRequestRequest{
				Url:      address,
				Method:   forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Headline: xptr.Ptr("Land the fix"),
				Body:     xptr.Ptr("Merges stop failing."),
				HeadSha:  "abc123",
			},
			wantMerged: []forge.MergeParams{{
				URL:      address,
				Method:   forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Headline: xptr.Ptr("Land the fix"),
				Body:     xptr.Ptr("Merges stop failing."),
				HeadSHA:  "abc123",
			}},
		},
		{
			name: "a body without a method",
			req: &forgev1.MergeForgePullRequestRequest{
				Url:  address,
				Body: xptr.Ptr("Merges stop failing."),
			},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeWriter{}
			service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(&fakeConnector{}, writer)}

			_, err := service.MergeForgePullRequest(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantMerged, writer.merged)
		})
	}
}

// TestSetForgePullRequestAutoMerge pins the method and message reaching the writer.
func TestSetForgePullRequestAutoMerge(t *testing.T) {
	const address = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/1"
	writer := &fakeWriter{}
	service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(&fakeConnector{}, writer)}

	_, err := service.SetForgePullRequestAutoMerge(t.Context(), connect.NewRequest(&forgev1.SetForgePullRequestAutoMergeRequest{
		Url:      address,
		On:       true,
		Method:   forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD,
		Headline: xptr.Ptr("Land the fix"),
	}))
	require.NoError(t, err)

	assert.Equal(t, []forge.SetAutoMergeParams{{
		URL:      address,
		On:       true,
		Method:   forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD,
		Headline: xptr.Ptr("Land the fix"),
	}}, writer.autoMerged)
}

// TestReadForgePullRequestText pins the text reaching the caller and a refusal reaching it as a
// failed precondition.
func TestReadForgePullRequestText(t *testing.T) {
	tests := []struct {
		name     string
		writer   *fakeWriter
		wantCode connect.Code
		wantText *forgev1.PullRequestText
	}{
		{
			name:     "an editable pull request",
			writer:   &fakeWriter{text: forge.Text{Title: "Land the fix", Body: "Merges stop failing."}},
			wantText: &forgev1.PullRequestText{Title: "Land the fix", Body: "Merges stop failing."},
		},
		{
			name:     "a pull request the credential may not change",
			writer:   &fakeWriter{err: forge.ErrNotEditable},
			wantCode: connect.CodeFailedPrecondition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(&fakeConnector{}, tt.writer)}

			resp, err := service.ReadForgePullRequestText(t.Context(), connect.NewRequest(&forgev1.ReadForgePullRequestTextRequest{
				Url: "https://github.com/harrisoncramer/komrad/pull/1",
			}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantText.GetTitle(), resp.Msg.GetTitle())
			assert.Equal(t, tt.wantText.GetBody(), resp.Msg.GetBody())
		})
	}
}

// TestEditForgePullRequest pins the fields reaching the writer and text that moved reaching the
// caller as a failed precondition.
func TestEditForgePullRequest(t *testing.T) {
	const address = "https://github.com/harrisoncramer/komrad/pull/1"
	read := &forgev1.PullRequestText{Title: "Land the fix", Body: "Merges stop failing."}

	tests := []struct {
		name       string
		req        *forgev1.EditForgePullRequestRequest
		err        error
		wantCode   connect.Code
		wantEdited []forge.EditParams
	}{
		{
			name: "a title alone",
			req:  &forgev1.EditForgePullRequestRequest{Url: address, EditedFrom: read, Title: xptr.Ptr("Land the merge fix")},
			wantEdited: []forge.EditParams{{
				URL:        address,
				EditedFrom: forge.Text{Title: "Land the fix", Body: "Merges stop failing."},
				Title:      xptr.Ptr("Land the merge fix"),
			}},
		},
		{
			name: "a description cleared",
			req:  &forgev1.EditForgePullRequestRequest{Url: address, EditedFrom: read, Body: xptr.Ptr("")},
			wantEdited: []forge.EditParams{{
				URL:        address,
				EditedFrom: forge.Text{Title: "Land the fix", Body: "Merges stop failing."},
				Body:       xptr.Ptr(""),
			}},
		},
		{
			name:     "text that moved since it was read",
			req:      &forgev1.EditForgePullRequestRequest{Url: address, EditedFrom: read, Title: xptr.Ptr("Land the merge fix")},
			err:      forge.ErrTextMoved,
			wantCode: connect.CodeFailedPrecondition,
			wantEdited: []forge.EditParams{{
				URL:        address,
				EditedFrom: forge.Text{Title: "Land the fix", Body: "Merges stop failing."},
				Title:      xptr.Ptr("Land the merge fix"),
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &fakeWriter{err: tt.err}
			service := forgev1komrad.ForgeServiceAdapter{Impl: NewHandler(&fakeConnector{}, writer)}

			_, err := service.EditForgePullRequest(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantEdited, writer.edited)
		})
	}
}

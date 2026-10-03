package gitlab

import (
	"cmp"
	"net/http"
	"testing"

	"komrad/internal/forge"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name:        "a merge request gitlab will not read back gives the reason from the refusal",
			answers:     map[string]answer{path + "/merge": refused},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab will not merge !7: 405 Method Not Allowed",
		},
		{
			name: "a merge request that became mergeable after the refusal gives the reason from the refusal",
			answers: map[string]answer{
				path + "/merge": {status: http.StatusConflict, body: `{"message":"SHA does not match HEAD of source branch"}`},
				path:            {body: `{"state":"opened","detailed_merge_status":"mergeable"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab will not merge !7: SHA does not match HEAD of source branch",
		},
		{
			name: "a merge request already being merged",
			answers: map[string]answer{
				path + "/merge": refused,
				path:            {body: `{"state":"locked","detailed_merge_status":"checking"}`},
			},
			wantErr:     forge.ErrNotMergeable,
			wantMessage: "gitlab will not merge !7: it is already being merged",
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
				assert.ErrorContains(t, err, tt.wantMessage)

				return
			}
			require.NoError(t, err)
		})
	}
}

// TestMergeabilityFolds covers the detailed merge statuses GitLab documents: one word each for
// mergeable, checks in the way, behind and conflicting, unknown while GitLab is still working
// it out or once the merge request has finished, and blocked for every other rule.
func TestMergeabilityFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state  string
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
		{
			state:  "merged",
			status: "MERGEABLE",
			want:   forge.MergeabilityUnknown,
		},
		{
			state:  "closed",
			status: "NOT_OPEN",
			want:   forge.MergeabilityUnknown,
		},
	}
	for _, tt := range tests {
		state := cmp.Or(tt.state, "opened")
		t.Run(state+" status "+tt.status, func(t *testing.T) {
			t.Parallel()

			node := mergeRequestNode{mergeReadinessNode: mergeReadinessNode{DetailedMergeStatus: tt.status}, State: state}
			assert.Equal(t, tt.want, node.mergeability())
		})
	}
}

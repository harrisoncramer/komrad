package gitlab

import (
	"cmp"
	"net/http"
	"slices"
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

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

	accepted := answer{body: `{"iid":7,"state":"merged"}`}

	planned := answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"state": "opened",
				"defaultMergeCommitMessage": "Merge branch 'fix-merge' into 'main'\n\nSee merge request harrisoncramer/komrad!7",
				"defaultSquashCommitMessage": "Fix the merge"
			}
		}
	}
}`}

	tests := []struct {
		name        string
		params      forge.MergeParams
		answers     map[string]answer
		wantErr     error
		wantMessage string
		wantBody    map[string]any
	}{
		{
			name:     "the merge request is accepted the way the project merges",
			answers:  map[string]answer{path + "/merge": accepted},
			wantBody: map[string]any{},
		},
		{
			name: "the merge request is squashed with the message asked for at the head the person saw",
			params: forge.MergeParams{
				Method:   forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Headline: xptr.Ptr("Land the fix"),
				Body:     xptr.Ptr("Merges stop failing."),
				HeadSHA:  "abc123",
			},
			answers:  map[string]answer{path + "/merge": accepted},
			wantBody: map[string]any{"squash": true, "squash_commit_message": "Land the fix\n\nMerges stop failing.", "sha": "abc123"},
		},
		{
			name: "the merge request is merged unsquashed with an empty body",
			params: forge.MergeParams{
				Method:   forgev1.MergeMethod_MERGE_METHOD_MERGE,
				Headline: xptr.Ptr("Land the fix"),
				Body:     xptr.Ptr(""),
			},
			answers:  map[string]answer{path + "/merge": accepted},
			wantBody: map[string]any{"squash": false, "merge_commit_message": "Land the fix"},
		},
		{
			name: "a headline alone keeps the body gitlab would write",
			params: forge.MergeParams{
				Method:   forgev1.MergeMethod_MERGE_METHOD_MERGE,
				Headline: xptr.Ptr("Land the fix"),
			},
			answers: map[string]answer{"MergePlan": planned, path + "/merge": accepted},
			wantBody: map[string]any{
				"squash":               false,
				"merge_commit_message": "Land the fix\n\nSee merge request harrisoncramer/komrad!7",
			},
		},
		{
			name: "a body alone keeps the headline gitlab would write",
			params: forge.MergeParams{
				Method: forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Body:   xptr.Ptr("Merges stop failing."),
			},
			answers:  map[string]answer{"MergePlan": planned, path + "/merge": accepted},
			wantBody: map[string]any{"squash": true, "squash_commit_message": "Fix the merge\n\nMerges stop failing."},
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

			params := tt.params
			params.URL = address
			err := f.Merge(t.Context(), params)
			put := slices.IndexFunc(fake.calls, func(seen call) bool { return seen.Method == http.MethodPut })
			require.NotEqual(t, -1, put)
			sent := fake.calls[put]
			assert.Equal(t, path+"/merge", sent.Path)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.ErrorContains(t, err, tt.wantMessage)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, sent.Body)
		})
	}
}

// TestMergeabilityFolds covers the detailed merge statuses GitLab documents: one word each for
// mergeable, behind and conflicting, unknown while GitLab is still working it out or once the
// merge request has finished, and blocked for every other rule.
func TestMergeabilityFolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state  string
		status string
		want   pullrequestv1.Mergeability
	}{
		{status: "MERGEABLE", want: pullrequestv1.Mergeability_MERGEABILITY_CLEAN},
		{status: "CI_MUST_PASS", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "CI_STILL_RUNNING", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "COMMITS_STATUS", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "NEED_REBASE", want: pullrequestv1.Mergeability_MERGEABILITY_BEHIND},
		{status: "CONFLICT", want: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING},
		{status: "UNCHECKED", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
		{status: "CHECKING", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
		{status: "PREPARING", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
		{status: "APPROVALS_SYNCING", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
		{status: "", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
		{status: "NOT_APPROVED", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "DISCUSSIONS_NOT_RESOLVED", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "DRAFT_STATUS", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "BLOCKED_STATUS", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "POLICIES_DENIED", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "REQUESTED_CHANGES", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{
			state:  "merged",
			status: "MERGEABLE",
			want:   pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
		},
		{
			state:  "closed",
			status: "NOT_OPEN",
			want:   pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
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

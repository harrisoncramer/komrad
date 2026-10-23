package gitlab

import (
	"net/http"
	"strconv"
	"testing"

	"komrad/internal/forge"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlanMerge covers offering squash beside the project's own merge method as far as the
// project lets the person choose, with the messages GitLab would write.
func TestPlanMerge(t *testing.T) {
	t.Parallel()

	const (
		address = "https://gitlab.com/harrisoncramer/komrad/-/merge_requests/7"
		project = "/projects/harrisoncramer%2Fkomrad"
	)

	target := func(state string, readOnly, onMerge bool) answer {
		return answer{body: `{
	"data": {
		"project": {
			"mergeRequest": {
				"state": "` + state + `",
				"diffHeadSha": "abc123",
				"squashReadOnly": ` + strconv.FormatBool(readOnly) + `,
				"squashOnMerge": ` + strconv.FormatBool(onMerge) + `,
				"defaultMergeCommitMessage": "Merge branch 'fix-merge' into 'main'\n\nFix the merge\n\nSee merge request harrisoncramer/komrad!7",
				"defaultSquashCommitMessage": "Fix the merge"
			}
		}
	}
}`}
	}
	mergeMessage := &forge.CommitMessage{
		Headline: "Merge branch 'fix-merge' into 'main'",
		Body:     "Fix the merge\n\nSee merge request harrisoncramer/komrad!7",
	}
	squashMessage := &forge.CommitMessage{Headline: "Fix the merge"}

	tests := []struct {
		name    string
		answers map[string]answer
		want    *forge.MergePlan
		wantErr error
	}{
		{
			name: "a project that lets the person squash or not",
			answers: map[string]answer{
				"MergePlan": target("opened", false, false),
				project:     {body: `{"merge_method":"rebase_merge"}`},
			},
			want: &forge.MergePlan{
				Options: []forge.MergeOption{
					{
						Method:  forgev1.MergeMethod_MERGE_METHOD_MERGE,
						Allowed: true,
						Message: mergeMessage,
					},
					{
						Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
						Allowed: true,
						Message: squashMessage,
					},
				},
				DefaultMethod: forgev1.MergeMethod_MERGE_METHOD_MERGE,
				HeadSHA:       "abc123",
			},
		},
		{
			name: "a project that always squashes",
			answers: map[string]answer{
				"MergePlan": target("opened", true, true),
				project:     {body: `{"merge_method":"merge"}`},
			},
			want: &forge.MergePlan{
				Options: []forge.MergeOption{
					{Method: forgev1.MergeMethod_MERGE_METHOD_MERGE, Message: mergeMessage},
					{
						Method:  forgev1.MergeMethod_MERGE_METHOD_SQUASH,
						Allowed: true,
						Message: squashMessage,
					},
				},
				DefaultMethod: forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				HeadSHA:       "abc123",
			},
		},
		{
			name: "a project that fast-forwards and never squashes",
			answers: map[string]answer{
				"MergePlan": target("opened", true, false),
				project:     {body: `{"merge_method":"ff"}`},
			},
			want: &forge.MergePlan{
				Options: []forge.MergeOption{
					{Method: forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD, Allowed: true},
					{Method: forgev1.MergeMethod_MERGE_METHOD_SQUASH, Message: squashMessage},
				},
				DefaultMethod: forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD,
				HeadSHA:       "abc123",
			},
		},
		{
			name:    "a merge request that has already merged",
			answers: map[string]answer{"MergePlan": target("merged", false, false)},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name:    "a merge request gitlab does not have",
			answers: map[string]answer{"MergePlan": {body: missingMergeRequest}},
			wantErr: forge.ErrNotFound,
		},
		{
			name: "a project the credential cannot read",
			answers: map[string]answer{
				"MergePlan": target("opened", false, false),
				project:     {status: http.StatusForbidden, body: `{"message":"403 Forbidden"}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, _ := serve(t, tt.answers)

			plan, err := f.PlanMerge(t.Context(), address)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, plan)
		})
	}
}

package github

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMerge covers merging with a method the base branch allows and GitHub's refusals.
func TestMerge(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	target := answer{body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"mergeCommitAllowed": true,
		"squashMergeAllowed": true,
		"rebaseMergeAllowed": false,
		"pullRequest": {
			"id": "PR_742",
			"state": "OPEN",
			"baseRef": {
				"rules": {
					"nodes": [
						{
							"type": "PULL_REQUEST",
							"parameters": {
								"allowedMergeMethods": [
									"SQUASH"
								]
							}
						},
						{
							"type": "DELETION",
							"parameters": null
						}
					]
				}
			}
		}
	}
}`)}

	merged := answer{body: data(`{
	"mergePullRequest": {
		"clientMutationId": null
	}
}`)}

	tests := []struct {
		name          string
		params        forge.MergeParams
		answers       map[string]answer
		wantErr       error
		wantVariables map[string]any
	}{
		{
			name: "the pull request merges with the method its base branch allows",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       merged,
			},
			wantVariables: map[string]any{"id": "PR_742", "method": "SQUASH", "headline": nil, "body": nil, "head": nil},
		},
		{
			name: "the pull request merges by the method and message asked for at the head the person saw",
			params: forge.MergeParams{
				Method:   forgev1.MergeMethod_MERGE_METHOD_REBASE,
				Headline: xptr.Ptr("Land the fix"),
				Body:     xptr.Ptr("Merges stop failing."),
				HeadSHA:  "abc123",
			},
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       merged,
			},
			wantVariables: map[string]any{"id": "PR_742", "method": "REBASE", "headline": "Land the fix", "body": "Merges stop failing.", "head": "abc123"},
		},
		{
			name: "a headline alone leaves the body to github",
			params: forge.MergeParams{
				Method:   forgev1.MergeMethod_MERGE_METHOD_SQUASH,
				Headline: xptr.Ptr("Land the fix"),
			},
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge":       merged,
			},
			wantVariables: map[string]any{"id": "PR_742", "method": "SQUASH", "headline": "Land the fix", "body": nil, "head": nil},
		},
		{
			name:    "a method github does not have is refused before github is asked to merge",
			params:  forge.MergeParams{Method: forgev1.MergeMethod_MERGE_METHOD_FAST_FORWARD},
			answers: map[string]answer{"MergeTarget": target},
			wantErr: forge.ErrNoMergeMethod,
		},
		{
			name: "a pull request github will not merge",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge": {body: `{
	"data": {
		"mergePullRequest": null
	},
	"errors": [
		{
			"type": "UNPROCESSABLE",
			"path": [
				"mergePullRequest"
			],
			"message": "Pull Request is not mergeable"
		}
	]
}`},
			},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a credential github will not let merge",
			answers: map[string]answer{
				"MergeTarget": target,
				"Merge": {body: `{
	"data": {
		"mergePullRequest": null
	},
	"errors": [
		{
			"type": "FORBIDDEN",
			"path": [
				"mergePullRequest"
			],
			"message": "Resource not accessible by integration"
		}
	]
}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name: "a pull request that has already merged is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"pullRequest": {
			"id": "PR_742",
			"state": "MERGED"
		}
	}
}`)}},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a closed pull request is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"pullRequest": {
			"id": "PR_742",
			"state": "CLOSED"
		}
	}
}`)}},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a pull request github does not have",
			answers: map[string]answer{
				"MergeTarget": {body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"pullRequest": null
	}
}`)},
			},
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
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVariables, fake.last(t).Variables)
		})
	}
}

// TestMergeRefusesWhatIsNotAPullRequest covers the addresses Merge will not ask GitHub about.
func TestMergeRefusesWhatIsNotAPullRequest(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, nil)

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

			err := f.Merge(t.Context(), forge.MergeParams{URL: tt.address})
			require.ErrorIs(t, err, forge.ErrNotPullRequest)
			assert.Empty(t, fake.calls)
		})
	}
}

// TestMergeabilityOf covers every merge state GitHub documents.
func TestMergeabilityOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   pullrequestv1.Mergeability
	}{
		{status: "CLEAN", want: pullrequestv1.Mergeability_MERGEABILITY_CLEAN},
		{status: "HAS_HOOKS", want: pullrequestv1.Mergeability_MERGEABILITY_CLEAN},
		{status: "UNSTABLE", want: pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE},
		{status: "BLOCKED", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "DRAFT", want: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		{status: "BEHIND", want: pullrequestv1.Mergeability_MERGEABILITY_BEHIND},
		{status: "DIRTY", want: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING},
		{status: "UNKNOWN", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
		{status: "", want: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, mergeabilityOf(tt.status))
		})
	}
}

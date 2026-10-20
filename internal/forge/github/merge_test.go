package github

import (
	"net/http"
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMerge covers merging with a method the base branch allows and GitHub's refusals.
func TestMerge(t *testing.T) {
	t.Parallel()

	const (
		address = "https://github.com/harrisoncramer/komrad/pull/742"
		stacks  = "/repos/harrisoncramer/komrad/stacks"
		merge   = "/repos/harrisoncramer/komrad/pulls/742/merge-async"
		status  = merge + "/7c1f0e2a"
	)
	target := answer{body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"mergeCommitAllowed": true,
		"squashMergeAllowed": true,
		"rebaseMergeAllowed": false,
		"pullRequest": {
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

	merged := answer{body: `{
	"status": "merged",
	"details": {
		"message": "Pull request successfully merged",
		"sha": "6dcb09b5b57875f334f61aebed695e2e4193db5e"
	}
}`}
	pending := answer{status: http.StatusAccepted, body: `{
	"status": "pending",
	"details": {
		"message": "Merge has been accepted and is being processed",
		"uuid": "7c1f0e2a",
		"merge_method": "squash"
	}
}`}

	tests := []struct {
		name     string
		params   forge.MergeParams
		answers  map[string]answer
		wantErr  error
		wantBody map[string]any
	}{
		{
			name:    "a method github does not have is refused before github is asked to merge",
			params:  forge.MergeParams{Method: forge.MergeMethodFastForward},
			answers: map[string]answer{"MergeTarget": target},
			wantErr: forge.ErrNoMergeMethod,
		},
		{
			name: "a pull request that has already merged is refused before github is asked to merge it",
			answers: map[string]answer{"MergeTarget": {body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"pullRequest": {
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
		{
			name: "a pull request with an open one under it in its stack is refused before github is asked to merge it",
			answers: map[string]answer{
				"MergeTarget": target,
				stacks: {body: `[
	{
		"number": 5,
		"open": true,
		"pull_requests": [
			{
				"number": 741,
				"state": "open",
				"merged_at": null
			},
			{
				"number": 742,
				"state": "open",
				"merged_at": null
			}
		]
	}
]`},
			},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a pull request github will not merge",
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         {status: http.StatusMethodNotAllowed, body: `{"message":"Pull Request is not mergeable"}`},
			},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "a credential github will not let merge",
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         {status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`},
			},
			wantErr: forge.ErrUnauthorized,
		},
		{
			name: "a merge github accepts and then fails",
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         pending,
				status: {body: `{
	"status": "failed",
	"details": {
		"message": "Head branch was modified. Review and try the merge again."
	}
}`},
			},
			wantErr: forge.ErrNotMergeable,
		},
		{
			name: "the pull request merges with the method its base branch allows",
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         merged,
			},
			wantBody: map[string]any{"merge_method": "squash"},
		},
		{
			name: "the pull request merges by the method and message asked for at the head the person saw",
			params: forge.MergeParams{
				Method:   forge.MergeMethodRebase,
				Headline: xptr.Ptr("Land the fix"),
				Body:     xptr.Ptr("Merges stop failing."),
				HeadSHA:  "abc123",
			},
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         merged,
			},
			wantBody: map[string]any{"merge_method": "rebase", "commit_title": "Land the fix", "commit_message": "Merges stop failing.", "sha": "abc123"},
		},
		{
			name: "a headline alone leaves the body to github",
			params: forge.MergeParams{
				Method:   forge.MergeMethodSquash,
				Headline: xptr.Ptr("Land the fix"),
			},
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         merged,
			},
			wantBody: map[string]any{"merge_method": "squash", "commit_title": "Land the fix"},
		},
		{
			name: "a merge github is still working on is left to the sync",
			answers: map[string]answer{
				"MergeTarget": target,
				merge:         pending,
				status:        pending,
			},
			wantBody: map[string]any{"merge_method": "squash"},
		},
		{
			name: "the pull request at the bottom of its stack merges",
			answers: map[string]answer{
				"MergeTarget": target,
				stacks: {body: `[
	{
		"number": 5,
		"open": true,
		"pull_requests": [
			{
				"number": 742,
				"state": "open",
				"merged_at": null
			},
			{
				"number": 743,
				"state": "open",
				"merged_at": null
			}
		]
	}
]`},
				merge: merged,
			},
			wantBody: map[string]any{"merge_method": "squash"},
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
			assert.Equal(t, tt.wantBody, fake.sentTo(t, merge).Body)
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
		want   forge.Mergeability
	}{
		{status: "CLEAN", want: forge.MergeabilityClean},
		{status: "HAS_HOOKS", want: forge.MergeabilityClean},
		{status: "UNSTABLE", want: forge.MergeabilityUnstable},
		{status: "BLOCKED", want: forge.MergeabilityBlocked},
		{status: "DRAFT", want: forge.MergeabilityBlocked},
		{status: "BEHIND", want: forge.MergeabilityBehind},
		{status: "DIRTY", want: forge.MergeabilityConflicting},
		{status: "UNKNOWN", want: forge.MergeabilityUnknown},
		{status: "", want: forge.MergeabilityUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, mergeabilityOf(tt.status))
		})
	}
}

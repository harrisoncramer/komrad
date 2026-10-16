package github

import (
	"testing"

	"komrad/internal/forge"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetAutoMerge covers queueing a pull request held to the head the person saw, withdrawing
// it, leaving one alone that already stands that way, and refusing a finished one before
// asking GitHub to change it.
func TestSetAutoMerge(t *testing.T) {
	t.Parallel()

	const address = "https://github.com/harrisoncramer/komrad/pull/742"
	target := func(state, request string) answer {
		return answer{body: data(`{
	"repository": {
		"viewerDefaultMergeMethod": "MERGE",
		"mergeCommitAllowed": true,
		"squashMergeAllowed": true,
		"rebaseMergeAllowed": true,
		"pullRequest": {
			"id": "PR_742",
			"state": "` + state + `",
			"baseRef": {
				"branchProtectionRule": {
					"requiresLinearHistory": true
				},
				"rules": {
					"nodes": [
						{
							"type": "PULL_REQUEST",
							"parameters": {
								"allowedMergeMethods": [
									"MERGE",
									"REBASE"
								]
							}
						}
					]
				}
			},
			"autoMergeRequest": ` + request + `
		}
	}
}`)}
	}
	queued := `{
	"enabledAt": "2026-09-20T12:00:00Z"
}`

	tests := []struct {
		name          string
		params        forge.SetAutoMergeParams
		answers       map[string]answer
		wantErr       error
		wantOperation string
		wantVariables map[string]any
	}{
		{
			name:   "a pull request github does not have",
			params: forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": {body: data(`{
	"repository": {
		"pullRequest": null
	}
}`)}},
			wantErr: forge.ErrNotFound,
		},
		{
			name:    "a merged pull request is refused before github is asked to change it",
			params:  forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{"AutoMergeTarget": target("MERGED", "null")},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:   "a pull request github will not queue",
			params: forge.SetAutoMergeParams{URL: address, On: true},
			answers: map[string]answer{
				"AutoMergeTarget": target("OPEN", "null"),
				"EnableAutoMerge": {body: `{
	"data": {
		"enablePullRequestAutoMerge": null
	},
	"errors": [
		{
			"type": "UNPROCESSABLE",
			"path": [
				"enablePullRequestAutoMerge"
			],
			"message": "Pull request is in clean status"
		}
	]
}`},
			},
			wantErr: forge.ErrNoAutoMerge,
		},
		{
			name:          "a queued pull request asked to be queued is left alone",
			params:        forge.SetAutoMergeParams{URL: address, On: true},
			answers:       map[string]answer{"AutoMergeTarget": target("OPEN", queued)},
			wantOperation: "AutoMergeTarget",
			wantVariables: map[string]any{"owner": "harrisoncramer", "name": "komrad", "number": float64(742)},
		},
		{
			name: "an open pull request is queued with the method its base branch allows at the head the person saw",
			params: forge.SetAutoMergeParams{
				URL:     address,
				HeadSHA: "abc123",
				On:      true,
			},
			answers: map[string]answer{
				"AutoMergeTarget": target("OPEN", "null"),
				"EnableAutoMerge": {body: data(`{
	"enablePullRequestAutoMerge": {
		"clientMutationId": null
	}
}`)},
			},
			wantOperation: "EnableAutoMerge",
			wantVariables: map[string]any{"id": "PR_742", "method": "REBASE", "headline": nil, "body": nil, "head": "abc123"},
		},
		{
			name: "an open pull request is queued by the method and message asked for",
			params: forge.SetAutoMergeParams{
				URL:      address,
				On:       true,
				Method:   forge.MergeMethodMerge,
				Headline: xptr.Ptr("Land the fix"),
			},
			answers: map[string]answer{
				"AutoMergeTarget": target("OPEN", "null"),
				"EnableAutoMerge": {body: data(`{
	"enablePullRequestAutoMerge": {
		"clientMutationId": null
	}
}`)},
			},
			wantOperation: "EnableAutoMerge",
			wantVariables: map[string]any{"id": "PR_742", "method": "MERGE", "headline": "Land the fix", "body": nil, "head": nil},
		},
		{
			name:   "a queued pull request is withdrawn",
			params: forge.SetAutoMergeParams{URL: address},
			answers: map[string]answer{
				"AutoMergeTarget": target("OPEN", queued),
				"DisableAutoMerge": {body: data(`{
	"disablePullRequestAutoMerge": {
		"clientMutationId": null
	}
}`)},
			},
			wantOperation: "DisableAutoMerge",
			wantVariables: map[string]any{"id": "PR_742"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, _, fake := serve(t, tt.answers)

			err := f.SetAutoMerge(t.Context(), tt.params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			seen := fake.last(t)
			assert.Equal(t, tt.wantOperation, seen.Operation)
			assert.Equal(t, tt.wantVariables, seen.Variables)
		})
	}
}

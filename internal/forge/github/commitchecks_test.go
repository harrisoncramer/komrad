package github

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadCommitChecks covers what a read of one commit's checks answers.
func TestReadCommitChecks(t *testing.T) {
	t.Parallel()

	const sha = "957dbee3c2ce0000000000000000000000000000"
	completedAt := time.Date(2026, 10, 3, 20, 32, 39, 0, time.UTC)
	tests := []struct {
		name    string
		checks  string
		steps   string
		want    *forge.Reading
		wantErr error
	}{
		{
			name: "a commit pushed over is read as the head of its pull request, with its steps",
			checks: data(`{
	"repository": {
		"object": {
			"oid": "` + sha + `",
			"statusCheckRollup": {
				"contexts": {
					"pageInfo": {
						"hasNextPage": true
					},
					"nodes": [
						{
							"__typename": "CheckRun",
							"id": "CR_1",
							"name": "test",
							"status": "COMPLETED",
							"conclusion": "CANCELLED",
							"completedAt": "2026-10-03T20:32:39Z",
							"isRequired": true
						}
					]
				}
			}
		}
	}
}`),
			steps: data(`{"s0": [{"id": "CR_1", "steps": {"pageInfo": {"hasNextPage": false}, "nodes": [{"number": 1, "name": "Run the unit tests", "status": "COMPLETED", "conclusion": "CANCELLED", "completedAt": "2026-10-03T20:32:39Z"}]}}]}`),
			want: &forge.Reading{PullRequests: []forge.PullRequest{{
				ProviderID: "PR_1",
				HeadSHA:    sha,
				State:      "OPEN",
				MergeReadiness: &forge.MergeReadiness{Listing: &forge.StatusCheckListing{
					Truncated: true,
					StatusChecks: []forge.StatusCheck{{
						ProviderID: "CR_1",
						Kind:       forge.StatusCheckKindRun,
						Name:       "test",
						State:      forge.StatusCheckStateCompleted,
						Conclusion: "cancelled",
						FinishedAt: &completedAt,
						Required:   true,
						Steps: []forge.StatusCheckStep{{
							Number:     1,
							Name:       "Run the unit tests",
							State:      forge.StatusCheckStateCompleted,
							Conclusion: "cancelled",
							FinishedAt: xptr.Ptr(completedAt),
						}},
					}},
				}},
			}}},
		},
		{
			name: "a commit with no checks is a blank listing",
			checks: data(`{
	"repository": {
		"object": {
			"oid": "` + sha + `",
			"statusCheckRollup": null
		}
	}
}`),
			want: &forge.Reading{PullRequests: []forge.PullRequest{{
				ProviderID:     "PR_1",
				HeadSHA:        sha,
				State:          "OPEN",
				MergeReadiness: &forge.MergeReadiness{Listing: &forge.StatusCheckListing{}},
			}}},
		},
		{
			name: "a commit github no longer holds is not found",
			checks: data(`{
	"repository": {
		"object": null
	}
}`),
			wantErr: forge.ErrNotFound,
		},
		{
			name: "a repository github does not show the credential is not found",
			checks: data(`{
	"repository": null
}`),
			wantErr: forge.ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			answers := map[string]answer{"ReadCommitChecks": {body: tt.checks}}
			if tt.steps != "" {
				answers["ReadCheckSteps"] = answer{body: tt.steps}
			}
			f, _, fake := serve(t, answers)

			got, err := f.ReadCommitChecks(t.Context(), forge.ReadCommitChecksParams{Repo: fixture, ProviderID: "PR_1", SHA: sha})
			seen := fake.calls[0]
			assert.Equal(t, "ReadCommitChecks", seen.Operation)
			assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "komrad-forge-fixture", "sha": sha, "pullRequest": "PR_1"}, seen.Variables)
			assert.Contains(t, seen.Query, "object(oid: $sha) { ... on Commit { "+commitChecksFields+" } }")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

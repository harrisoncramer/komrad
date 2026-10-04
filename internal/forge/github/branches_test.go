package github

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadBranches covers the aliased query, each answer carrying its merge readiness, the checks on
// each open head, the rate limit off the headers, and only pull requests from the repository itself kept.
func TestReadBranches(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"ReadBranches": {
			header: map[string]string{"X-RateLimit-Remaining": "4322", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1789900000"},
			body: branchesRead(
				withMergeReadiness(openPull, failingMergeReadiness)+","+withMergeReadiness(forkPull, failingMergeReadiness),
				withMergeReadiness(deletedForkPull, passingMergeReadiness),
				withMergeReadiness(closedFixturePull, passingMergeReadiness),
			),
		},
		"ReadStatusChecks": {
			header: map[string]string{"X-RateLimit-Remaining": "4321", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1789900000"},
			body:   data(openPullChecks),
		},
		"ReadCheckSteps": {
			header: map[string]string{"X-RateLimit-Remaining": "4320", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1789900000"},
			body:   data(`{"s0": [{"id": "CR_1", "steps": {"pageInfo": {"hasNextPage": false}, "nodes": []}}, {"id": "CR_2", "steps": {"pageInfo": {"hasNextPage": false}, "nodes": []}}]}`),
		},
	})

	reading, err := f.ReadBranches(t.Context(), fixture, []string{"fixture/open", "client-tests", "fixture/closed"})
	require.NoError(t, err)

	require.Len(t, fake.calls, 3)
	checksSeen := fake.calls[1]
	assert.Equal(t, "ReadStatusChecks", checksSeen.Operation)
	assert.Equal(t, map[string]any{"p0": "PR_kwDOUgkrbM8AAAABEIdmHw"}, checksSeen.Variables, "only the open pull request is asked after")
	assert.Contains(t, checksSeen.Query, "query ReadStatusChecks($p0: ID!) {")
	assert.Contains(t, checksSeen.Query, "p0: node(id: $p0) { ... on PullRequest { "+strings.ReplaceAll(statusCheckListFields, "$pullRequest", "$p0")+" } }")

	stepsSeen := fake.calls[2]
	assert.Equal(t, "ReadCheckSteps", stepsSeen.Operation)
	assert.Equal(t, map[string]any{"s0": []any{"CR_1", "CR_2"}}, stepsSeen.Variables, "only the check runs are asked after")
	assert.Contains(t, stepsSeen.Query, "query ReadCheckSteps($s0: [ID!]!) {")
	assert.Contains(t, stepsSeen.Query, "s0: nodes(ids: $s0) { "+checkStepsFields+" }")

	seen := fake.calls[0]
	assert.Equal(t, "ReadBranches", seen.Operation)
	assert.Equal(t, map[string]any{
		"owner": "harrisoncramer",
		"name":  "komrad-forge-fixture",
		"b0":    "fixture/open",
		"b1":    "client-tests",
		"b2":    "fixture/closed",
	}, seen.Variables)
	assert.Contains(t, seen.Query, "query ReadBranches($owner: String!, $name: String!, $b0: String!, $b1: String!, $b2: String!)")
	assert.Contains(t, seen.Query, "b0: pullRequests(headRefName: $b0, first: 5, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { ...pullRequestFields ...mergeReadinessFields } }")
	assert.Contains(t, seen.Query, pullRequestFragment)
	assert.Contains(t, seen.Query, mergeReadinessFragment)

	assert.Equal(t, forge.RateLimit{
		Known:     true,
		Remaining: 4320,
		Limit:     5000,
		ResetAt:   time.Unix(1789900000, 0),
	}, reading.RateLimit)

	require.Len(t, reading.PullRequests, 2, "the fork and the deleted fork are left out")
	open, closed := reading.PullRequests[0], reading.PullRequests[1]
	assert.Equal(t, 1, open.Number)
	assert.Equal(t, &forge.MergeReadiness{
		Decision:     forge.ReviewDecisionChangesRequested,
		Mergeability: forge.MergeabilityUnstable,
		AutoMerge:    forge.AutoMerge{Allowed: true},
		Listing: &forge.StatusCheckListing{StatusChecks: []forge.StatusCheck{
			{
				ProviderID:   "CR_1",
				Kind:         forge.StatusCheckKindRun,
				Source:       "CI",
				Name:         "test",
				State:        forge.StatusCheckStateCompleted,
				Conclusion:   "failure",
				StartedAt:    xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
				FinishedAt:   xptr.Ptr(time.Date(2026, 9, 20, 11, 5, 0, 0, time.UTC)),
				Required:     true,
				URL:          "https://github.com/harrisoncramer/komrad-forge-fixture/actions/runs/1",
				RunID:        "CS_1",
				RunEvent:     "pull_request",
				RunStartedAt: xptr.Ptr(time.Date(2026, 9, 20, 10, 59, 0, 0, time.UTC)),
			},
			{
				ProviderID: "CR_2",
				Kind:       forge.StatusCheckKindRun,
				Source:     "Scanner",
				Name:       "scan",
				State:      forge.StatusCheckStateQueued,
				RunID:      "CS_2",
			},
			{
				ProviderID: "SC_1",
				Kind:       forge.StatusCheckKindStatus,
				Name:       "ci/legacy",
				State:      forge.StatusCheckStateCompleted,
				Conclusion: "error",
				FinishedAt: xptr.Ptr(time.Date(2026, 9, 20, 11, 2, 0, 0, time.UTC)),
				Required:   true,
				URL:        "https://ci.example.com/1",
			},
		}},
	}, open.MergeReadiness)
	assert.Equal(t, 2, closed.Number)
	assert.Equal(t, &forge.MergeReadiness{
		Decision:     forge.ReviewDecisionApproved,
		Mergeability: forge.MergeabilityClean,
		AutoMerge:    forge.AutoMerge{On: true},
	}, closed.MergeReadiness)
}

// TestReadBranchesBatches covers more branches than one query takes going out as a second query.
func TestReadBranchesBatches(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"ReadBranches": {body: branchesRead(withMergeReadiness(openPull, failingMergeReadiness))},
		"ReadStatusChecks": {body: data(`{
	"p0": null
}`)},
	})

	branches := make([]string, branchesPerQuery+1)
	for i := range branches {
		branches[i] = "branch-" + strconv.Itoa(i)
	}
	reading, err := f.ReadBranches(t.Context(), fixture, branches)
	require.NoError(t, err)

	require.Len(t, fake.calls, 4, "each batch reads the checks of its own open pull requests")
	assert.Equal(t, []string{"ReadBranches", "ReadStatusChecks", "ReadBranches", "ReadStatusChecks"}, xslice.Map(fake.calls, func(seen call) string { return seen.Operation }))
	assert.Len(t, fake.calls[0].Variables, branchesPerQuery+2)
	assert.Equal(t, "branch-49", fake.calls[0].Variables["b49"])
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "komrad-forge-fixture", "b0": "branch-50"}, fake.calls[2].Variables)
	assert.Len(t, reading.PullRequests, 2)
}

// TestReadBranchesRefusals covers a repository GitHub does not have and a path it cannot
// address, the last costing no request.
func TestReadBranchesRefusals(t *testing.T) {
	t.Parallel()

	missing, _, _ := serve(t, map[string]answer{"ReadBranches": {body: `{
	"data": {
		"repository": null
	},
	"errors": [
		{
			"type": "NOT_FOUND",
			"path": [
				"repository"
			],
			"locations": [
				{
					"line": 2,
					"column": 3
				}
			],
			"message": "Could not resolve to a Repository with the name 'harrisoncramer/gone'."
		}
	]
}`}})
	_, err := missing.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "harrisoncramer/gone"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotFound)

	f, _, fake := serve(t, nil)
	_, err = f.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

// passingMergeReadiness is the merge readiness fields of an approved pull request whose checks passed.
const passingMergeReadiness = `	"reviewDecision": "APPROVED",
	"mergeStateStatus": "CLEAN",
	"autoMergeRequest": {
		"enabledAt": "2026-09-20T11:00:00Z"
	},
	"viewerCanEnableAutoMerge": false`

const closedFixturePull = `{
	"id": "PR_kwDOUgkrbM8AAAABEIdmIA",
	"number": 2,
	"title": "fixture: closed",
	"url": "https://github.com/harrisoncramer/komrad-forge-fixture/pull/2",
	"state": "CLOSED",
	"isDraft": false,
	"createdAt": "2026-09-17T10:00:00Z",
	"updatedAt": "2026-09-17T11:00:00Z",
	"mergedAt": null,
	"closedAt": "2026-09-17T11:00:00Z",
	"headRefName": "fixture/closed",
	"headRefOid": "5555555555555555555555555555555555555555",
	"baseRefName": "main",
	"baseRefOid": "8b32d7985e62febbf494e815545c22b446276b5d",
	"headRepository": {
		"url": "https://github.com/harrisoncramer/komrad-forge-fixture"
	},
	"baseRepository": {
		"url": "https://github.com/harrisoncramer/komrad-forge-fixture"
	}
}`

// openPullChecks is GitHub answering a read of checks on openPull's head, one of each kind.
const openPullChecks = `{
	"p0": {
		"commits": {
			"nodes": [
				{
					"commit": {
						"oid": "acc6d4fd6caf3511c998a673406e6fed2df7a28b",
						"statusCheckRollup": {
							"contexts": {
								"nodes": [
									{
										"__typename": "CheckRun",
										"id": "CR_1",
										"name": "test",
										"status": "COMPLETED",
										"conclusion": "FAILURE",
										"startedAt": "2026-09-20T11:00:00Z",
										"completedAt": "2026-09-20T11:05:00Z",
										"detailsUrl": "https://github.com/harrisoncramer/komrad-forge-fixture/actions/runs/1",
										"checkSuite": {
											"id": "CS_1",
											"createdAt": "2026-09-20T10:59:00Z",
											"app": {
												"name": "GitHub Actions"
											},
											"workflowRun": {
												"event": "pull_request",
												"workflow": {
													"name": "CI"
												}
											}
										},
										"isRequired": true
									},
									{
										"__typename": "CheckRun",
										"id": "CR_2",
										"name": "scan",
										"status": "QUEUED",
										"conclusion": null,
										"startedAt": null,
										"completedAt": null,
										"detailsUrl": null,
										"checkSuite": {
											"id": "CS_2",
											"app": {
												"name": "Scanner"
											},
											"workflowRun": null
										},
										"isRequired": false
									},
									{
										"__typename": "StatusContext",
										"id": "SC_1",
										"context": "ci/legacy",
										"state": "ERROR",
										"targetUrl": "https://ci.example.com/1",
										"createdAt": "2026-09-20T11:02:00Z",
										"isRequired": true
									},
									{
										"__typename": "CheckRunLike"
									}
								]
							}
						}
					}
				}
			]
		}
	}
}`

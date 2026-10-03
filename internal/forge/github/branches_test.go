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
		"ReadChecks": {
			header: map[string]string{"X-RateLimit-Remaining": "4321", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1789900000"},
			body:   data(openPullChecks),
		},
	})

	reading, err := f.ReadBranches(t.Context(), fixture, []string{"fixture/open", "client-tests", "fixture/closed"})
	require.NoError(t, err)

	require.Len(t, fake.calls, 2)
	checksSeen := fake.calls[1]
	assert.Equal(t, "ReadChecks", checksSeen.Operation)
	assert.Equal(t, map[string]any{"p0": "PR_kwDOUgkrbM8AAAABEIdmHw"}, checksSeen.Variables, "only the open pull request is asked after")
	assert.Contains(t, checksSeen.Query, "query ReadChecks($p0: ID!) {")
	assert.Contains(t, checksSeen.Query, "p0: node(id: $p0) { ... on PullRequest { "+strings.ReplaceAll(checkListFields, "$pullRequest", "$p0")+" } }")

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
	assert.Contains(t, seen.Query, "b0: pullRequests(headRefName: $b0, first: 5, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { ...pullRequestFields "+mergeReadinessFields+" } }")
	assert.Contains(t, seen.Query, pullRequestFragment)

	assert.Equal(t, forge.RateLimit{
		Known:     true,
		Remaining: 4321,
		Limit:     5000,
		ResetAt:   time.Unix(1789900000, 0),
	}, reading.RateLimit)

	require.Len(t, reading.PullRequests, 2, "the fork and the deleted fork are left out")
	open, closed := reading.PullRequests[0], reading.PullRequests[1]
	assert.Equal(t, 1, open.Number)
	assert.Equal(t, &forge.MergeReadiness{
		Decision:     forge.ReviewDecisionChangesRequested,
		Checks:       forge.ChecksFailing,
		Mergeability: forge.MergeabilityUnstable,
		AutoMerge:    forge.AutoMerge{Allowed: true},
		Listing: &forge.CheckListing{Checks: []forge.Check{
			{
				ProviderID: "CR_1",
				Kind:       forge.CheckKindRun,
				Source:     "CI",
				Name:       "test",
				State:      forge.CheckStateCompleted,
				Conclusion: "failure",
				StartedAt:  xptr.Ptr(time.Date(2026, 9, 20, 11, 0, 0, 0, time.UTC)),
				FinishedAt: xptr.Ptr(time.Date(2026, 9, 20, 11, 5, 0, 0, time.UTC)),
				Required:   true,
				URL:        "https://github.com/harrisoncramer/komrad-forge-fixture/actions/runs/1",
			},
			{
				ProviderID: "CR_2",
				Kind:       forge.CheckKindRun,
				Source:     "Scanner",
				Name:       "scan",
				State:      forge.CheckStateQueued,
			},
			{
				ProviderID: "SC_1",
				Kind:       forge.CheckKindStatus,
				Name:       "ci/legacy",
				State:      forge.CheckStateCompleted,
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
		Checks:       forge.ChecksPassing,
		Mergeability: forge.MergeabilityClean,
		AutoMerge:    forge.AutoMerge{On: true},
	}, closed.MergeReadiness)
}

// TestReadBranchesBatches covers more branches than one query takes going out as a second query.
func TestReadBranchesBatches(t *testing.T) {
	t.Parallel()

	f, _, fake := serve(t, map[string]answer{
		"ReadBranches": {body: branchesRead(withMergeReadiness(openPull, failingMergeReadiness))},
		"ReadChecks":   {body: data(`{"p0":null}`)},
	})

	branches := make([]string, branchesPerQuery+1)
	for i := range branches {
		branches[i] = "branch-" + strconv.Itoa(i)
	}
	reading, err := f.ReadBranches(t.Context(), fixture, branches)
	require.NoError(t, err)

	require.Len(t, fake.calls, 4, "each batch reads the checks of its own open pull requests")
	assert.Equal(t, []string{"ReadBranches", "ReadChecks", "ReadBranches", "ReadChecks"}, xslice.Map(fake.calls, func(seen call) string { return seen.Operation }))
	assert.Len(t, fake.calls[0].Variables, branchesPerQuery+2)
	assert.Equal(t, "branch-49", fake.calls[0].Variables["b49"])
	assert.Equal(t, map[string]any{"owner": "harrisoncramer", "name": "komrad-forge-fixture", "b0": "branch-50"}, fake.calls[2].Variables)
	assert.Len(t, reading.PullRequests, 2)
}

// TestReadBranchesRefusals covers a repository GitHub does not have and a path it cannot
// address, the last costing no request.
func TestReadBranchesRefusals(t *testing.T) {
	t.Parallel()

	missing, _, _ := serve(t, map[string]answer{"ReadBranches": {body: `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","path":["repository"],"locations":[{"line":2,"column":3}],"message":"Could not resolve to a Repository with the name 'harrisoncramer/gone'."}]}`}})
	_, err := missing.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "harrisoncramer/gone"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotFound)

	f, _, fake := serve(t, nil)
	_, err = f.ReadBranches(t.Context(), git.RemoteURL{Host: "github.com", Path: "group/sub/repo"}, []string{"main"})
	require.ErrorIs(t, err, forge.ErrNotRepository)
	assert.Empty(t, fake.calls)
}

package ui

import (
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPullRequestWords covers the state word with what is still in the way beside it.
func TestPullRequestWords(t *testing.T) {
	type testCase struct {
		name        string
		pullRequest panel.PullRequest
		want        string
	}
	tests := []testCase{
		{
			name: "a row with its checks running names itself once",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
				MergeReadiness: panel.MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_PENDING,
				},
			},
			want: "checks running",
		},
		{
			name: "a change request with its checks running says both",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
				MergeReadiness: panel.MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_PENDING,
				},
			},
			want: "changes requested, checks running",
		},
		{
			name: "an approval behind its base",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
				MergeReadiness: panel.MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BEHIND,
				},
			},
			want: "approved, behind the base",
		},
		{
			name: "an approval the forge has not computed yet says so",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
				MergeReadiness: panel.MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
				},
			},
			want: "approved, forge still computing",
		},
		{
			name: "an approval still blocked by a rule the forge is rechecking says the rule",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
				MergeReadiness: panel.MergeReadiness{
					Checks:                  pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability:            pullrequestv1.Mergeability_MERGEABILITY_BLOCKED,
					MergeabilityUnconfirmed: true,
				},
			},
			want: "approved, blocked by a rule",
		},
		{
			name: "a conflicting row says so and nothing about its checks",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
				MergeReadiness: panel.MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PENDING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
				},
			},
			want: "conflicts with the base",
		},
		{
			name: "a draft conflicting with its base keeps the draft word and says so",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
				MergeReadiness: panel.MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PENDING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
				},
			},
			want: "draft, conflicts with the base",
		},
		{
			name: "a failing run names itself once",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
				MergeReadiness: panel.MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_FAILING,
				},
			},
			want: "checks failing",
		},
		{
			name: "a failing run with checks still going says so",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
				MergeReadiness: panel.MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_FAILING_RUNNING,
				},
			},
			want: "checks failing, others still running",
		},
		{
			name: "a ready branch the forge is rechecking says so",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				MergeReadiness: panel.MergeReadiness{
					Checks:                  pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability:            pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
					MergeabilityUnconfirmed: true,
				},
			},
			want: "ready to merge, forge rechecking",
		},
		{
			name: "a branch the forge would merge says nothing more",
			pullRequest: panel.PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				MergeReadiness: panel.MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
				},
			},
			want: "ready to merge",
		},
		{
			name:        "a state a newer server added says it is unknown",
			pullRequest: panel.PullRequest{State: pullrequestv1.PullRequestState(99)},
			want:        "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestWords(tt.pullRequest))
		})
	}
}

// TestPullRequestFields covers the current pull request's title and author and the row for the
// rest, which takes every one once the branch has moved past its last.
func TestPullRequestFields(t *testing.T) {
	yesterday := time.Now().Add(-26 * time.Hour)
	current := panel.PullRequest{Number: 742, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED}

	tests := []struct {
		name         string
		state        pullrequestv1.PullRequestState
		pullRequests []panel.PullRequest
		want         []recordField
	}{
		{
			name: "none",
		},
		{
			name:         "only the current one",
			pullRequests: []panel.PullRequest{current},
		},
		{
			name: "the current one's title and author",
			pullRequests: []panel.PullRequest{
				{
					Number: 742,
					Title:  "Read pull request authors",
					Author: "harrisoncramer",
				},
			},
			want: []recordField{
				{label: "title", value: "Read pull request authors"},
				{label: "author", value: "@harrisoncramer"},
			},
		},
		{
			name: "an empty title and author left out",
			pullRequests: []panel.PullRequest{
				{Number: 742, Author: "harrisoncramer"},
			},
			want: []recordField{{label: "author", value: "@harrisoncramer"}},
		},
		{
			name: "the earlier ones folded into one row",
			pullRequests: []panel.PullRequest{
				current,
				{
					Number:   711,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
				{
					Number:   708,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
					OpenedAt: yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []recordField{{label: "pull requests", value: "#711 merged, #708 closed"}},
		},
		{
			name:  "a branch moved past its last one shows no current title or author",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
			pullRequests: []panel.PullRequest{
				{
					Number:   711,
					Title:    "Read pull request authors",
					Author:   "harrisoncramer",
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []recordField{{label: "pull requests", value: "#711 merged"}},
		},
		{
			name:  "a branch on GitLab names its merge requests the way GitLab does",
			state: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
			pullRequests: []panel.PullRequest{
				{
					Forge:    "gitlab",
					Number:   711,
					State:    pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
					OpenedAt: yesterday,
					MergedAt: &yesterday,
					ClosedAt: &yesterday,
				},
			},
			want: []recordField{{label: "merge requests", value: "!711 merged"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pullRequestFields(panel.PullRequestStanding{State: tt.state}, tt.pullRequests))
		})
	}
}

// TestAgentFieldsPlaceThePullRequestsAfterTheBranch covers the rows landing under the branch
// they are about, and not at all on an agent whose checkout is on no branch.
func TestAgentFieldsPlaceThePullRequestsAfterTheBranch(t *testing.T) {
	agent := panel.Agent{
		Name:   "worker",
		Branch: xptr.Ptr("agent/x"),
	}
	pullRequests := []panel.PullRequest{
		{
			Number: 7,
			Title:  "Fix",
			Author: "octocat",
			State:  pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{Number: 5, State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED},
	}

	labels := readLabels(agentFields(agentFieldsParams{agent: agent, pullRequests: pullRequests}))

	branch := indexOf(labels, "branch")
	require.NotEqual(t, -1, branch)
	assert.Equal(t, []string{"title", "author", "pull requests"}, labels[branch+1:branch+4])

	agent.Branch = nil
	labels = readLabels(agentFields(agentFieldsParams{agent: agent, pullRequests: pullRequests}))
	for _, absent := range []string{"title", "author", "pull requests"} {
		assert.NotContains(t, labels, absent)
	}
}

func readLabels(fields []recordField) []string {
	return xslice.Map(fields, func(field recordField) string {
		return field.label
	})
}

func indexOf(labels []string, label string) int {
	for i, candidate := range labels {
		if candidate == label {
			return i
		}
	}

	return -1
}

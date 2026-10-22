package panel

import (
	"testing"

	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestPullRequestWords covers the state word with what is still in the way beside it.
func TestPullRequestWords(t *testing.T) {
	type testCase struct {
		name        string
		pullRequest PullRequest
		want        string
	}
	tests := []testCase{
		{
			name: "a row with its checks running names itself once",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
				MergeReadiness: MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_PENDING,
				},
			},
			want: "checks running",
		},
		{
			name: "a change request with its checks running says both",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
				MergeReadiness: MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_PENDING,
				},
			},
			want: "changes requested, checks running",
		},
		{
			name: "an approval behind its base",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
				MergeReadiness: MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BEHIND,
				},
			},
			want: "approved, behind the base",
		},
		{
			name: "an approval the forge has not computed yet says so",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
				MergeReadiness: MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN,
				},
			},
			want: "approved, forge still computing",
		},
		{
			name: "an approval still blocked by a rule the forge is rechecking says the rule",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
				MergeReadiness: MergeReadiness{
					Checks:                  pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability:            pullrequestv1.Mergeability_MERGEABILITY_BLOCKED,
					MergeabilityUnconfirmed: true,
				},
			},
			want: "approved, blocked by a rule",
		},
		{
			name: "a conflicting row says so and nothing about its checks",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
				MergeReadiness: MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PENDING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
				},
			},
			want: "conflicts with the base",
		},
		{
			name: "a draft conflicting with its base keeps the draft word and says so",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
				MergeReadiness: MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PENDING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
				},
			},
			want: "draft, conflicts with the base",
		},
		{
			name: "a failing run names itself once",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
				MergeReadiness: MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_FAILING,
				},
			},
			want: "checks failing",
		},
		{
			name: "a failing run with checks still going says so",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
				MergeReadiness: MergeReadiness{
					Checks: pullrequestv1.Checks_CHECKS_FAILING_RUNNING,
				},
			},
			want: "checks failing, others still running",
		},
		{
			name: "a ready branch the forge is rechecking says so",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				MergeReadiness: MergeReadiness{
					Checks:                  pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability:            pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
					MergeabilityUnconfirmed: true,
				},
			},
			want: "ready to merge, forge rechecking",
		},
		{
			name: "a branch the forge would merge says nothing more",
			pullRequest: PullRequest{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
				MergeReadiness: MergeReadiness{
					Checks:       pullrequestv1.Checks_CHECKS_PASSING,
					Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
				},
			},
			want: "ready to merge",
		},
		{
			name:        "a state a newer server added says it is unknown",
			pullRequest: PullRequest{State: pullrequestv1.PullRequestState(99)},
			want:        "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.pullRequest.Words())
		})
	}
}

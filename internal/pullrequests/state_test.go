package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestStateOf pins each state against the one ranked just below it.
func TestStateOf(t *testing.T) {
	stamp := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		pullRequest pullrequeststore.PullRequest
		checks      pullrequestv1.Checks
		read        forge.MergeReadiness
		readHead    string
		want        pullrequestv1.PullRequestState
	}{
		{
			name: "merged beats closed",
			pullRequest: pullrequeststore.PullRequest{
				MergedAt: &stamp,
				ClosedAt: &stamp,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		},
		{
			name: "closed beats a draft",
			pullRequest: pullrequeststore.PullRequest{
				ClosedAt: &stamp,
				Draft:    true,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
		},
		{
			name:        "a draft beats failing checks",
			pullRequest: pullrequeststore.PullRequest{Draft: true},
			checks:      pullrequestv1.Checks_CHECKS_FAILING,
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		},
		{
			name:   "a failure with others still running is failing",
			checks: pullrequestv1.Checks_CHECKS_FAILING_RUNNING,
			want:   pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name:   "failing checks beat a conflict",
			checks: pullrequestv1.Checks_CHECKS_FAILING,
			read: forge.MergeReadiness{
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name: "a conflict beats a change request",
			read: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED,
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
		},
		{
			name: "a change request stands over a clean branch",
			read: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED,
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name:   "a change request beats checks running",
			checks: pullrequestv1.Checks_CHECKS_PENDING,
			read: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name:   "a re-review request beats checks running",
			checks: pullrequestv1.Checks_CHECKS_PENDING,
			read: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED,
		},
		{
			name:   "failing checks beat a re-review request",
			checks: pullrequestv1.Checks_CHECKS_FAILING,
			read: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name:   "checks running beat an approval on a clean branch",
			checks: pullrequestv1.Checks_CHECKS_PENDING,
			read: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
		},
		{
			name:   "clean beats an approval that would otherwise wait",
			checks: pullrequestv1.Checks_CHECKS_PASSING,
			read: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name:   "clean with no decision is ready, since the forge's rules asked for no review",
			checks: pullrequestv1.Checks_CHECKS_PASSING,
			read: forge.MergeReadiness{
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name: "clean with no checks reported is open, since checks may not have started",
			read: forge.MergeReadiness{
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name: "approved and clean with no checks reported is waiting",
			read: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name:   "approved and behind is waiting though its checks pass",
			checks: pullrequestv1.Checks_CHECKS_PASSING,
			read: forge.MergeReadiness{
				Decision:     pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BEHIND,
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name:        "a conflict read for an older head is not a conflict",
			pullRequest: pullrequeststore.PullRequest{HeadSHA: "bbb"},
			read:        forge.MergeReadiness{Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING},
			readHead:    "aaa",
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name: "a change request the forge no longer reports is open",
			pullRequest: pullrequeststore.PullRequest{
				MergeReadiness: forge.RememberedMergeReadiness{}.RememberRead(
					forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED},
					"",
					stamp,
				),
			},
			read: forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name: "a pending review is open",
			read: forge.MergeReadiness{Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := tt.pullRequest
			pullRequest.MergeReadiness = pullRequest.MergeReadiness.RememberRead(tt.read, tt.readHead, stamp)
			pullRequest.MergeReadiness.Checks = forge.RememberedAnswer[pullrequestv1.Checks]{Value: tt.checks, HeadSHA: tt.readHead}

			assert.Equal(t, tt.want, StateOf(pullRequest))
		})
	}
}

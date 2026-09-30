package render

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestPullRequestStateOf pins each state against the one ranked just below it.
func TestPullRequestStateOf(t *testing.T) {
	stamp := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		pullRequest pullrequeststore.PullRequest
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
			name: "a draft beats failing checks",
			pullRequest: pullrequeststore.PullRequest{
				Draft:  true,
				Checks: xptr.Ptr(forge.ChecksFailing),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		},
		{
			name: "failing checks beat a conflict",
			pullRequest: pullrequeststore.PullRequest{
				Checks:       xptr.Ptr(forge.ChecksFailing),
				Mergeability: xptr.Ptr(forge.MergeabilityConflicting),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name: "a conflict beats a change request",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Mergeability:   xptr.Ptr(forge.MergeabilityConflicting),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
		},
		{
			name: "a change request stands over a clean branch",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name: "a change request beats checks running",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Checks:         xptr.Ptr(forge.ChecksPending),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name: "checks running beat an approval on a clean branch",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPending),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
		},
		{
			name: "clean beats an approval that would otherwise wait",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name: "clean with no decision is ready, since the forge's rules asked for no review",
			pullRequest: pullrequeststore.PullRequest{
				Mergeability: xptr.Ptr(forge.MergeabilityClean),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name: "approved and behind is waiting though its checks pass",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPassing),
				Mergeability:   xptr.Ptr(forge.MergeabilityBehind),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name:        "a pending review is open",
			pullRequest: pullrequeststore.PullRequest{ReviewDecision: xptr.Ptr(forge.ReviewDecisionPending)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PullRequestStateOf(tt.pullRequest))
		})
	}
}

// TestMergeabilityOf covers each stored word and a row the forge has not computed.
func TestMergeabilityOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN, MergeabilityOf(nil))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CLEAN, MergeabilityOf(xptr.Ptr(forge.MergeabilityClean)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE, MergeabilityOf(xptr.Ptr(forge.MergeabilityUnstable)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BLOCKED, MergeabilityOf(xptr.Ptr(forge.MergeabilityBlocked)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BEHIND, MergeabilityOf(xptr.Ptr(forge.MergeabilityBehind)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING, MergeabilityOf(xptr.Ptr(forge.MergeabilityConflicting)))
}

// TestChecksOf covers each stored word and a row the forge reports no checks on.
func TestChecksOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Checks_CHECKS_NONE, ChecksOf(nil))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PENDING, ChecksOf(xptr.Ptr(forge.ChecksPending)))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PASSING, ChecksOf(xptr.Ptr(forge.ChecksPassing)))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_FAILING, ChecksOf(xptr.Ptr(forge.ChecksFailing)))
}

package render

import (
	"testing"
	"time"

	"weave/internal/forge"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/lib/xptr"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestPullRequestStateOf covers every combination of the stamps, the draft flag, the checks,
// the mergeability and the decision: finished beats draft beats failing checks beats the
// decision, and an approval waits until the forge would merge it.
func TestPullRequestStateOf(t *testing.T) {
	stamp := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		pullRequest pullrequeststore.Summary
		want        pullrequestv1.PullRequestState
	}{
		{
			name: "merged beats everything, closed_at included",
			pullRequest: pullrequeststore.Summary{
				MergedAt:       &stamp,
				ClosedAt:       &stamp,
				Draft:          true,
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		},
		{
			name: "closed without merging beats draft and the decision",
			pullRequest: pullrequeststore.Summary{
				ClosedAt:       &stamp,
				Draft:          true,
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
		},
		{
			name: "a draft beats its decision",
			pullRequest: pullrequeststore.Summary{
				Draft:          true,
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		},
		{
			name: "a draft beats its failing checks",
			pullRequest: pullrequeststore.Summary{
				Draft:  true,
				Checks: xptr.Ptr(forge.ChecksFailing),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		},
		{
			name: "failing checks beat an approval",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksFailing),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name: "failing checks beat a change request",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Checks:         xptr.Ptr(forge.ChecksFailing),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name:        "failing checks with no decision",
			pullRequest: pullrequeststore.Summary{Checks: xptr.Ptr(forge.ChecksFailing)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name:        "approved",
			pullRequest: pullrequeststore.Summary{ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name: "approved with passing checks",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPassing),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name: "approved with checks still running and no mergeability is waiting",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPending),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name: "approved and clean is approved",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPassing),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name: "approved and behind is waiting though its checks pass",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPassing),
				Mergeability:   xptr.Ptr(forge.MergeabilityBehind),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name: "approved and blocked is waiting",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Mergeability:   xptr.Ptr(forge.MergeabilityBlocked),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name: "approved and conflicting is waiting",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Mergeability:   xptr.Ptr(forge.MergeabilityConflicting),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		},
		{
			name: "failing checks beat a clean mergeability",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksFailing),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		},
		{
			name: "conflicting with no decision is open",
			pullRequest: pullrequeststore.Summary{
				Mergeability: xptr.Ptr(forge.MergeabilityConflicting),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name: "a change request stands while checks run",
			pullRequest: pullrequeststore.Summary{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Checks:         xptr.Ptr(forge.ChecksPending),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name:        "changes requested",
			pullRequest: pullrequeststore.Summary{ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name:        "a pending review is open",
			pullRequest: pullrequeststore.Summary{ReviewDecision: xptr.Ptr(forge.ReviewDecisionPending)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name:        "no decision is open",
			pullRequest: pullrequeststore.Summary{},
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

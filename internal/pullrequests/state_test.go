package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/stretchr/testify/assert"
)

// TestStateOf pins each state against the one ranked just below it.
func TestStateOf(t *testing.T) {
	stamp := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		pullRequest pullrequeststore.PullRequest
		read        forge.MergeReadiness
		readHead    string
		want        State
	}{
		{
			name: "merged beats closed",
			pullRequest: pullrequeststore.PullRequest{
				MergedAt: &stamp,
				ClosedAt: &stamp,
			},
			want: StateMerged,
		},
		{
			name: "closed beats a draft",
			pullRequest: pullrequeststore.PullRequest{
				ClosedAt: &stamp,
				Draft:    true,
			},
			want: StateClosed,
		},
		{
			name:        "a draft beats failing checks",
			pullRequest: pullrequeststore.PullRequest{Draft: true},
			read: forge.MergeReadiness{
				Checks: forge.ChecksFailing,
			},
			want: StateDraft,
		},
		{
			name: "a failure with others still running is failing",
			read: forge.MergeReadiness{
				Checks: forge.ChecksFailingRunning,
			},
			want: StateFailing,
		},
		{
			name: "failing checks beat a conflict",
			read: forge.MergeReadiness{
				Checks:       forge.ChecksFailing,
				Mergeability: forge.MergeabilityConflicting,
			},
			want: StateFailing,
		},
		{
			name: "a conflict beats a change request",
			read: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionChangesRequested,
				Mergeability: forge.MergeabilityConflicting,
			},
			want: StateConflicting,
		},
		{
			name: "a change request stands over a clean branch",
			read: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionChangesRequested,
				Mergeability: forge.MergeabilityClean,
			},
			want: StateChangesRequested,
		},
		{
			name: "a change request beats checks running",
			read: forge.MergeReadiness{
				Decision: forge.ReviewDecisionChangesRequested,
				Checks:   forge.ChecksPending,
			},
			want: StateChangesRequested,
		},
		{
			name: "a re-review request beats checks running",
			read: forge.MergeReadiness{
				Decision: forge.ReviewDecisionRereviewRequested,
				Checks:   forge.ChecksPending,
			},
			want: StateRereviewRequested,
		},
		{
			name: "failing checks beat a re-review request",
			read: forge.MergeReadiness{
				Decision: forge.ReviewDecisionRereviewRequested,
				Checks:   forge.ChecksFailing,
			},
			want: StateFailing,
		},
		{
			name: "checks running beat an approval on a clean branch",
			read: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionApproved,
				Checks:       forge.ChecksPending,
				Mergeability: forge.MergeabilityClean,
			},
			want: StateChecksRunning,
		},
		{
			name: "clean beats an approval that would otherwise wait",
			read: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionApproved,
				Checks:       forge.ChecksPassing,
				Mergeability: forge.MergeabilityClean,
			},
			want: StateApproved,
		},
		{
			name: "clean with no decision is ready, since the forge's rules asked for no review",
			read: forge.MergeReadiness{
				Checks:       forge.ChecksPassing,
				Mergeability: forge.MergeabilityClean,
			},
			want: StateApproved,
		},
		{
			name: "clean with no checks reported is open, since checks may not have started",
			read: forge.MergeReadiness{
				Mergeability: forge.MergeabilityClean,
			},
			want: StateOpen,
		},
		{
			name: "approved and clean with no checks reported is waiting",
			read: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionApproved,
				Mergeability: forge.MergeabilityClean,
			},
			want: StateApprovedWaiting,
		},
		{
			name: "approved and behind is waiting though its checks pass",
			read: forge.MergeReadiness{
				Decision:     forge.ReviewDecisionApproved,
				Checks:       forge.ChecksPassing,
				Mergeability: forge.MergeabilityBehind,
			},
			want: StateApprovedWaiting,
		},
		{
			name:        "a conflict read for an older head is not a conflict",
			pullRequest: pullrequeststore.PullRequest{HeadSHA: "bbb"},
			read:        forge.MergeReadiness{Mergeability: forge.MergeabilityConflicting},
			readHead:    "aaa",
			want:        StateOpen,
		},
		{
			name: "a change request the forge no longer reports is open",
			pullRequest: pullrequeststore.PullRequest{
				MergeReadiness: forge.RememberedMergeReadiness{}.RememberRead(
					forge.MergeReadiness{Decision: forge.ReviewDecisionChangesRequested},
					"",
					stamp,
				),
			},
			read: forge.MergeReadiness{Decision: forge.ReviewDecisionNone},
			want: StateOpen,
		},
		{
			name: "a pending review is open",
			read: forge.MergeReadiness{Decision: forge.ReviewDecisionPending},
			want: StateOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := tt.pullRequest
			pullRequest.MergeReadiness = pullRequest.MergeReadiness.RememberRead(tt.read, tt.readHead, stamp)

			assert.Equal(t, tt.want, StateOf(pullRequest))
		})
	}
}

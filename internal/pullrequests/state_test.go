package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestStateOf pins each state against the one ranked just below it.
func TestStateOf(t *testing.T) {
	stamp := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		pullRequest pullrequeststore.PullRequest
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
			name: "a draft beats failing checks",
			pullRequest: pullrequeststore.PullRequest{
				Draft:  true,
				Checks: xptr.Ptr(forge.ChecksFailing),
			},
			want: StateDraft,
		},
		{
			name: "failing checks beat a conflict",
			pullRequest: pullrequeststore.PullRequest{
				Checks:       xptr.Ptr(forge.ChecksFailing),
				Mergeability: xptr.Ptr(forge.MergeabilityConflicting),
			},
			want: StateFailing,
		},
		{
			name: "a conflict beats a change request",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Mergeability:   xptr.Ptr(forge.MergeabilityConflicting),
			},
			want: StateConflicting,
		},
		{
			name: "a change request stands over a clean branch",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: StateChangesRequested,
		},
		{
			name: "a change request beats checks running",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionChangesRequested),
				Checks:         xptr.Ptr(forge.ChecksPending),
			},
			want: StateChangesRequested,
		},
		{
			name: "a re-review request beats checks running",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionRereviewRequested),
				Checks:         xptr.Ptr(forge.ChecksPending),
			},
			want: StateRereviewRequested,
		},
		{
			name: "failing checks beat a re-review request",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionRereviewRequested),
				Checks:         xptr.Ptr(forge.ChecksFailing),
			},
			want: StateFailing,
		},
		{
			name: "checks running beat an approval on a clean branch",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPending),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: StateChecksRunning,
		},
		{
			name: "clean beats an approval that would otherwise wait",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Mergeability:   xptr.Ptr(forge.MergeabilityClean),
			},
			want: StateApproved,
		},
		{
			name: "clean with no decision is ready, since the forge's rules asked for no review",
			pullRequest: pullrequeststore.PullRequest{
				Mergeability: xptr.Ptr(forge.MergeabilityClean),
			},
			want: StateApproved,
		},
		{
			name: "approved and behind is waiting though its checks pass",
			pullRequest: pullrequeststore.PullRequest{
				ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:         xptr.Ptr(forge.ChecksPassing),
				Mergeability:   xptr.Ptr(forge.MergeabilityBehind),
			},
			want: StateApprovedWaiting,
		},
		{
			name:        "a pending review is open",
			pullRequest: pullrequeststore.PullRequest{ReviewDecision: xptr.Ptr(forge.ReviewDecisionPending)},
			want:        StateOpen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StateOf(tt.pullRequest))
		})
	}
}

// TestResolveMergeability covers a mergeability the forge is working out again, taken from its
// last answer for the same head and marked unconfirmed, and never from an answer for another head.
func TestResolveMergeability(t *testing.T) {
	type testCase struct {
		name             string
		pullRequest      pullrequeststore.PullRequest
		wantState        State
		wantMergeability *forge.Mergeability
		wantUnconfirmed  bool
	}
	tests := []testCase{
		{
			name: "a last known clean for another head is not used",
			pullRequest: pullrequeststore.PullRequest{
				HeadSHA:                  "def",
				Checks:                   xptr.Ptr(forge.ChecksPassing),
				LastKnownMergeability:    "clean",
				LastKnownMergeabilitySHA: "abc",
			},
			wantState:        StateOpen,
			wantMergeability: nil,
			wantUnconfirmed:  false,
		},
		{
			name: "a last known conflict on the same head still conflicts",
			pullRequest: pullrequeststore.PullRequest{
				HeadSHA:                  "abc",
				Checks:                   xptr.Ptr(forge.ChecksPassing),
				LastKnownMergeability:    "conflicting",
				LastKnownMergeabilitySHA: "abc",
			},
			wantState:        StateConflicting,
			wantMergeability: xptr.Ptr(forge.MergeabilityConflicting),
			wantUnconfirmed:  true,
		},
		{
			name: "a last known behind on the same head still waits",
			pullRequest: pullrequeststore.PullRequest{
				HeadSHA:                  "abc",
				ReviewDecision:           xptr.Ptr(forge.ReviewDecisionApproved),
				Checks:                   xptr.Ptr(forge.ChecksPassing),
				LastKnownMergeability:    "behind",
				LastKnownMergeabilitySHA: "abc",
			},
			wantState:        StateApprovedWaiting,
			wantMergeability: xptr.Ptr(forge.MergeabilityBehind),
			wantUnconfirmed:  true,
		},
		{
			name: "a latest read is confirmed",
			pullRequest: pullrequeststore.PullRequest{
				HeadSHA:                  "abc",
				Checks:                   xptr.Ptr(forge.ChecksPassing),
				Mergeability:             xptr.Ptr(forge.MergeabilityBlocked),
				LastKnownMergeability:    "clean",
				LastKnownMergeabilitySHA: "abc",
			},
			wantState:        StateOpen,
			wantMergeability: xptr.Ptr(forge.MergeabilityBlocked),
			wantUnconfirmed:  false,
		},
		{
			name: "a last known clean on the same head is ready",
			pullRequest: pullrequeststore.PullRequest{
				HeadSHA:                  "abc",
				Checks:                   xptr.Ptr(forge.ChecksPassing),
				LastKnownMergeability:    "clean",
				LastKnownMergeabilitySHA: "abc",
			},
			wantState:        StateApproved,
			wantMergeability: xptr.Ptr(forge.MergeabilityClean),
			wantUnconfirmed:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mergeability, unconfirmed := ResolveMergeability(tt.pullRequest)

			assert.Equal(t, tt.wantState, StateOf(tt.pullRequest))
			assert.Equal(t, tt.wantMergeability, mergeability)
			assert.Equal(t, tt.wantUnconfirmed, unconfirmed)
		})
	}
}

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

// TestPullRequestStateOf covers every combination of the stamps, the draft flag and the
// decision, finished beating draft beating the decision.
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
			name:        "approved",
			pullRequest: pullrequeststore.Summary{ReviewDecision: xptr.Ptr(forge.ReviewDecisionApproved)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
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

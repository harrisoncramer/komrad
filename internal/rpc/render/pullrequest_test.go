package render

import (
	"testing"
	"time"

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
		pullRequest pullrequeststore.PullRequest
		want        pullrequestv1.PullRequestState
	}{
		{
			name: "merged beats everything, closed_at included",
			pullRequest: pullrequeststore.PullRequest{
				MergedAt:       &stamp,
				ClosedAt:       &stamp,
				Draft:          true,
				ReviewDecision: xptr.Ptr(DecisionChangesRequested),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		},
		{
			name: "closed without merging beats draft and the decision",
			pullRequest: pullrequeststore.PullRequest{
				ClosedAt:       &stamp,
				Draft:          true,
				ReviewDecision: xptr.Ptr(DecisionApproved),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
		},
		{
			name: "a draft beats its decision",
			pullRequest: pullrequeststore.PullRequest{
				Draft:          true,
				ReviewDecision: xptr.Ptr(DecisionApproved),
			},
			want: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		},
		{
			name:        "approved",
			pullRequest: pullrequeststore.PullRequest{ReviewDecision: xptr.Ptr(DecisionApproved)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		},
		{
			name:        "changes requested",
			pullRequest: pullrequeststore.PullRequest{ReviewDecision: xptr.Ptr(DecisionChangesRequested)},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		},
		{
			name:        "review required is open",
			pullRequest: pullrequeststore.PullRequest{ReviewDecision: xptr.Ptr("REVIEW_REQUIRED")},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
		{
			name:        "no decision is open",
			pullRequest: pullrequeststore.PullRequest{},
			want:        pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PullRequestStateOf(tt.pullRequest))
		})
	}
}

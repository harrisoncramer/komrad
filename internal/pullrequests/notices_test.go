package pullrequests

import (
	"testing"

	"komrad/internal/notices"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/stretchr/testify/assert"
)

// TestNoticesOf pins the notices a pull request change starts and those still holding.
func TestNoticesOf(t *testing.T) {
	tests := []struct {
		name        string
		moves       []Move
		pullRequest pullrequeststore.PullRequest
		wantStarted []string
		wantHolding []string
	}{
		{
			name:        "a move to a notice-worthy value starts it",
			moves:       []Move{{Field: FieldChecks, From: "pending", To: "failing"}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN", LastKnownChecks: "failing"},
			wantStarted: []string{notices.PullRequestChecksFailing},
			wantHolding: []string{notices.PullRequestChecksFailing},
		},
		{
			name:        "a move away starts nothing and leaves what still holds",
			moves:       []Move{{Field: FieldChecks, From: "failing", To: "passing"}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN", LastKnownChecks: "passing", LastKnownMergeability: "behind"},
			wantHolding: []string{notices.PullRequestBehind},
		},
		{
			name:        "nothing holding is an empty list rather than none",
			moves:       []Move{{Field: FieldDecision, From: "approved", To: "review_required"}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN", LastKnownDecision: "review_required"},
			wantHolding: []string{},
		},
		{
			name: "a merge is the only notice, whatever else moved",
			moves: []Move{
				{Field: FieldState, From: "OPEN", To: "MERGED"},
				{Field: FieldDecision, From: "review_required", To: "approved"},
			},
			pullRequest: pullrequeststore.PullRequest{State: "MERGED", LastKnownDecision: "approved", LastKnownChecks: "failing"},
			wantStarted: []string{notices.PullRequestMerged},
			wantHolding: []string{notices.PullRequestMerged},
		},
		{
			name:        "and so is a close",
			moves:       []Move{{Field: FieldState, From: "OPEN", To: "CLOSED"}},
			pullRequest: pullrequeststore.PullRequest{State: "CLOSED", LastKnownMergeability: "conflicting"},
			wantStarted: []string{notices.PullRequestClosed},
			wantHolding: []string{notices.PullRequestClosed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started, holding := noticesOf(Change{Moves: tt.moves}, tt.pullRequest)
			assert.Equal(t, tt.wantStarted, started)
			assert.Equal(t, tt.wantHolding, holding)
		})
	}
}

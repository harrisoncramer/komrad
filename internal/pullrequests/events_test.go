package pullrequests

import (
	"testing"

	"komrad/internal/eventprompts"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
)

// TestEventsOf pins the events a pull request change starts and those still holding.
func TestEventsOf(t *testing.T) {
	tests := []struct {
		name        string
		moves       []Move
		pullRequest pullrequeststore.PullRequest
		wantStarted []string
		wantHolding []string
	}{
		{
			name:        "a move to an event-worthy value starts it",
			moves:       []Move{{Field: FieldChecks, From: "pending", To: "failing"}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN", LastKnownChecks: "failing"},
			wantStarted: []string{eventprompts.PullRequestChecksFailing},
			wantHolding: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name:        "a move away starts nothing and leaves what still holds",
			moves:       []Move{{Field: FieldChecks, From: "failing", To: "passing"}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN", LastKnownChecks: "passing", LastKnownMergeability: "behind"},
			wantHolding: []string{eventprompts.PullRequestBehind},
		},
		{
			name:        "turning auto-merge on starts it",
			moves:       []Move{{Field: FieldAutoMerge, From: autoMergeOff, To: autoMergeOn}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN", AutoMerge: xptr.Ptr(true)},
			wantStarted: []string{eventprompts.PullRequestAutoMergeOn},
			wantHolding: []string{eventprompts.PullRequestAutoMergeOn},
		},
		{
			name: "a merge is the only event, whatever else moved",
			moves: []Move{
				{Field: FieldState, From: "OPEN", To: "MERGED"},
				{Field: FieldDecision, From: "review_required", To: "approved"},
			},
			pullRequest: pullrequeststore.PullRequest{State: "MERGED", LastKnownDecision: "approved", LastKnownChecks: "failing"},
			wantStarted: []string{eventprompts.PullRequestMerged},
			wantHolding: []string{eventprompts.PullRequestMerged},
		},
		{
			name:        "a change after the merge starts nothing and keeps the merge alone",
			moves:       []Move{{Field: FieldChecks, From: "passing", To: "failing"}},
			pullRequest: pullrequeststore.PullRequest{State: "MERGED", LastKnownChecks: "failing"},
			wantHolding: []string{eventprompts.PullRequestMerged},
		},
		{
			name:        "and so is a close",
			moves:       []Move{{Field: FieldState, From: "OPEN", To: "CLOSED"}},
			pullRequest: pullrequeststore.PullRequest{State: "CLOSED", LastKnownMergeability: "conflicting"},
			wantStarted: []string{eventprompts.PullRequestClosed},
			wantHolding: []string{eventprompts.PullRequestClosed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started, holding := eventsOf(Change{Moves: tt.moves}, tt.pullRequest)
			assert.Equal(t, tt.wantStarted, started)
			assert.Equal(t, tt.wantHolding, holding)
		})
	}
}

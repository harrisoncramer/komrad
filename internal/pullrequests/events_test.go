package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/eventprompts"
	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/stretchr/testify/assert"
)

// TestEventsOf pins the events a pull request change starts and those still holding.
func TestEventsOf(t *testing.T) {
	tests := []struct {
		name        string
		moves       []Move
		pullRequest pullrequeststore.PullRequest
		read        forge.MergeReadiness
		wantStarted []string
		wantHolding []string
	}{
		{
			name: "a move to an event-worthy value starts it",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "failing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			read:        forge.MergeReadiness{Checks: forge.ChecksFailing},
			wantStarted: []string{eventprompts.PullRequestChecksFailing},
			wantHolding: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name: "a move away starts nothing and leaves what still holds",
			moves: []Move{{
				Field: fieldChecks,
				From:  "failing",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			read: forge.MergeReadiness{
				Checks:       forge.ChecksPassing,
				Mergeability: forge.MergeabilityBehind,
			},
			wantHolding: []string{eventprompts.PullRequestBehind},
		},
		{
			name: "turning auto-merge on starts it",
			moves: []Move{{
				Field: fieldAutoMerge,
				From:  "off",
				To:    "on",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			read:        forge.MergeReadiness{AutoMerge: forge.AutoMerge{On: true}},
			wantStarted: []string{eventprompts.PullRequestAutoMergeOn},
			wantHolding: []string{eventprompts.PullRequestAutoMergeOn},
		},
		{
			name: "a merge is the only event, whatever else moved",
			moves: []Move{
				{
					Field: fieldState,
					From:  "OPEN",
					To:    "MERGED",
				},
				{
					Field: fieldDecision,
					From:  "review_required",
					To:    "approved",
				},
			},
			pullRequest: pullrequeststore.PullRequest{State: "MERGED"},
			read: forge.MergeReadiness{
				Decision: forge.ReviewDecisionApproved,
				Checks:   forge.ChecksFailing,
			},
			wantStarted: []string{eventprompts.PullRequestMerged},
			wantHolding: []string{eventprompts.PullRequestMerged},
		},
		{
			name: "a change after the merge starts nothing and keeps the merge alone",
			moves: []Move{{
				Field: fieldChecks,
				From:  "passing",
				To:    "failing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "MERGED"},
			read:        forge.MergeReadiness{Checks: forge.ChecksFailing},
			wantHolding: []string{eventprompts.PullRequestMerged},
		},
		{
			name: "and so is a close",
			moves: []Move{{
				Field: fieldState,
				From:  "OPEN",
				To:    "CLOSED",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "CLOSED"},
			read:        forge.MergeReadiness{Mergeability: forge.MergeabilityConflicting},
			wantStarted: []string{eventprompts.PullRequestClosed},
			wantHolding: []string{eventprompts.PullRequestClosed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := tt.pullRequest
			pullRequest.MergeReadiness = forge.RememberedMergeReadiness{}.RememberRead(tt.read, pullRequest.HeadSHA, time.Time{})

			started, holding := eventsOf(Change{Moves: tt.moves}, pullRequest)
			assert.Equal(t, tt.wantStarted, started)
			assert.Equal(t, tt.wantHolding, holding)
		})
	}
}

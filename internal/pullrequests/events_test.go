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
		checks      forge.Checks
		read        forge.MergeReadiness
		wantStarted []string
		wantHolding []string
	}{
		{
			name: "a move of the checks to failing starts nothing but holds it",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "failing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksFailing,
			wantHolding: []string{eventprompts.PullRequestChecksFailing},
		},
		{
			name: "and so does a failure while others still run",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "failing_running",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksFailingRunning,
			wantHolding: []string{eventprompts.PullRequestChecksFailingRunning},
		},
		{
			name: "a move away starts nothing and leaves what still holds",
			moves: []Move{{
				Field: fieldChecks,
				From:  "failing",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksPassing,
			read: forge.MergeReadiness{
				Mergeability: forge.MergeabilityBehind,
			},
			wantHolding: []string{eventprompts.PullRequestBehind},
		},
		{
			name: "checks going green while the forge blocks the merge start nothing",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksPassing,
			read:        forge.MergeReadiness{Mergeability: forge.MergeabilityBlocked},
		},
		{
			name: "checks going green on a mergeable pull request start passing",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksPassing,
			read:        forge.MergeReadiness{Mergeability: forge.MergeabilityClean},
			wantStarted: []string{eventprompts.PullRequestChecksPassing},
			wantHolding: []string{eventprompts.PullRequestChecksPassing},
		},
		{
			name: "the forge unblocking green checks starts passing",
			moves: []Move{{
				Field: fieldMergeability,
				From:  "blocked",
				To:    "clean",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksPassing,
			read:        forge.MergeReadiness{Mergeability: forge.MergeabilityClean},
			wantStarted: []string{eventprompts.PullRequestChecksPassing},
			wantHolding: []string{eventprompts.PullRequestChecksPassing},
		},
		{
			name: "the forge unblocking pending checks starts nothing",
			moves: []Move{{
				Field: fieldMergeability,
				From:  "blocked",
				To:    "clean",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      forge.ChecksPending,
			read:        forge.MergeReadiness{Mergeability: forge.MergeabilityClean},
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
			checks:      forge.ChecksFailing,
			read: forge.MergeReadiness{
				Decision: forge.ReviewDecisionApproved,
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
			checks:      forge.ChecksFailing,
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
			pullRequest.MergeReadiness.Checks = forge.RememberedAnswer[forge.Checks]{Value: tt.checks, HeadSHA: pullRequest.HeadSHA}

			started, holding := eventsOf(Change{Moves: tt.moves}, pullRequest)
			assert.Equal(t, tt.wantStarted, started)
			assert.Equal(t, tt.wantHolding, holding)
		})
	}
}

// TestFailingEvent covers the event a failed check the agent was not told of starts: the one for the checks word,
// and none once the checks are not failing or the pull request ended.
func TestFailingEvent(t *testing.T) {
	tests := []struct {
		name   string
		state  string
		checks forge.Checks
		want   string
	}{
		{
			name:   "failing",
			state:  "OPEN",
			checks: forge.ChecksFailing,
			want:   eventprompts.PullRequestChecksFailing,
		},
		{
			name:   "failing while others still run",
			state:  "OPEN",
			checks: forge.ChecksFailingRunning,
			want:   eventprompts.PullRequestChecksFailingRunning,
		},
		{
			name:   "passing",
			state:  "OPEN",
			checks: forge.ChecksPassing,
		},
		{
			name:   "merged",
			state:  "MERGED",
			checks: forge.ChecksFailing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := pullrequeststore.PullRequest{State: tt.state}
			pullRequest.MergeReadiness.Checks = forge.RememberedAnswer[forge.Checks]{Value: tt.checks}

			assert.Equal(t, tt.want, failingEvent(pullRequest))
		})
	}
}

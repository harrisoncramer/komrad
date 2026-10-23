package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	eventv1 "komrad/proto/gen/event/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestEventsOf pins the events a pull request change starts and those still holding.
func TestEventsOf(t *testing.T) {
	tests := []struct {
		name        string
		moves       []Move
		pullRequest pullrequeststore.PullRequest
		checks      pullrequestv1.Checks
		read        forge.MergeReadiness
		wantStarted []eventv1.Event
		wantHolding []eventv1.Event
	}{
		{
			name: "a move of the checks to failing starts nothing but holds it",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "failing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_FAILING,
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING},
		},
		{
			name: "and so does a failure while others still run",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "failing_running",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_FAILING_RUNNING,
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING_RUNNING},
		},
		{
			name: "a move away starts nothing and leaves what still holds",
			moves: []Move{{
				Field: fieldChecks,
				From:  "failing",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_PASSING,
			read: forge.MergeReadiness{
				Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BEHIND,
			},
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_BEHIND},
		},
		{
			name: "checks going green while the forge blocks the merge start nothing",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_PASSING,
			read:        forge.MergeReadiness{Mergeability: pullrequestv1.Mergeability_MERGEABILITY_BLOCKED},
		},
		{
			name: "checks going green on a mergeable pull request start passing",
			moves: []Move{{
				Field: fieldChecks,
				From:  "pending",
				To:    "passing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_PASSING,
			read:        forge.MergeReadiness{Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN},
			wantStarted: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING},
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING},
		},
		{
			name: "the forge unblocking green checks starts passing",
			moves: []Move{{
				Field: fieldMergeability,
				From:  "blocked",
				To:    "clean",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_PASSING,
			read:        forge.MergeReadiness{Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN},
			wantStarted: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING},
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING},
		},
		{
			name: "the forge unblocking pending checks starts nothing",
			moves: []Move{{
				Field: fieldMergeability,
				From:  "blocked",
				To:    "clean",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "OPEN"},
			checks:      pullrequestv1.Checks_CHECKS_PENDING,
			read:        forge.MergeReadiness{Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CLEAN},
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
			wantStarted: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_AUTO_MERGE_ON},
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_AUTO_MERGE_ON},
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
			checks:      pullrequestv1.Checks_CHECKS_FAILING,
			read: forge.MergeReadiness{
				Decision: pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED,
			},
			wantStarted: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_MERGED},
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_MERGED},
		},
		{
			name: "a change after the merge starts nothing and keeps the merge alone",
			moves: []Move{{
				Field: fieldChecks,
				From:  "passing",
				To:    "failing",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "MERGED"},
			checks:      pullrequestv1.Checks_CHECKS_FAILING,
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_MERGED},
		},
		{
			name: "and so is a close",
			moves: []Move{{
				Field: fieldState,
				From:  "OPEN",
				To:    "CLOSED",
			}},
			pullRequest: pullrequeststore.PullRequest{State: "CLOSED"},
			read:        forge.MergeReadiness{Mergeability: pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING},
			wantStarted: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CLOSED},
			wantHolding: []eventv1.Event{eventv1.Event_EVENT_PULL_REQUEST_CLOSED},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := tt.pullRequest
			pullRequest.MergeReadiness = forge.RememberedMergeReadiness{}.RememberRead(tt.read, pullRequest.HeadSHA, time.Time{})
			pullRequest.MergeReadiness.Checks = forge.RememberedAnswer[pullrequestv1.Checks]{Value: tt.checks, HeadSHA: pullRequest.HeadSHA}

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
		checks pullrequestv1.Checks
		want   eventv1.Event
	}{
		{
			name:   "failing",
			state:  "OPEN",
			checks: pullrequestv1.Checks_CHECKS_FAILING,
			want:   eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING,
		},
		{
			name:   "failing while others still run",
			state:  "OPEN",
			checks: pullrequestv1.Checks_CHECKS_FAILING_RUNNING,
			want:   eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING_RUNNING,
		},
		{
			name:   "passing",
			state:  "OPEN",
			checks: pullrequestv1.Checks_CHECKS_PASSING,
		},
		{
			name:   "merged",
			state:  "MERGED",
			checks: pullrequestv1.Checks_CHECKS_FAILING,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pullRequest := pullrequeststore.PullRequest{State: tt.state}
			pullRequest.MergeReadiness.Checks = forge.RememberedAnswer[pullrequestv1.Checks]{Value: tt.checks}

			assert.Equal(t, tt.want, failingEvent(pullRequest))
		})
	}
}

package ui

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"komrad/internal/ui/panel"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestTriageRefresh covers which agents need triage, for what reason and since when.
func TestTriageRefresh(t *testing.T) {
	changed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	earlier := changed.Add(-time.Hour)
	seen := changed.Add(time.Hour)
	live := func(state panel.State) panel.AgentRow {
		return panel.AgentRow{
			Agent: panel.Agent{
				ID:             "a1",
				State:          state,
				StateChangedAt: changed,
			},
			Alive: true,
		}
	}
	idle := func(change func(*panel.AgentRow)) panel.AgentRow {
		item := live(panel.StateIdle)
		change(&item)

		return item
	}
	pullRequest := func(state pullrequestv1.PullRequestState) panel.AgentRow {
		return idle(func(item *panel.AgentRow) { item.PullRequest.State = state })
	}
	implementer := func(change func(*panel.AgentRow)) panel.AgentRow {
		return idle(func(item *panel.AgentRow) {
			item.Agent.Role = panel.RoleImplementer
			change(item)
		})
	}
	child := func(state panel.State) panel.AgentRow {
		return panel.AgentRow{
			Agent: panel.Agent{
				ID:            "c1",
				State:         state,
				ParentAgentID: xptr.Ptr("a1"),
			},
			Alive: true,
		}
	}
	findRule := func(name string) triageRule {
		for _, rule := range triageRules {
			if rule.name == name {
				return rule
			}
		}

		t.Fatalf("no triage rule %q", name)

		return triageRule{}
	}

	tests := []struct {
		name string
		was  map[string]triageEntry
		item panel.AgentRow
		// others are the rest of the listing beside the agent.
		others []panel.AgentRow
		want   string
		// since is when the agent entered triage, unchecked when it is not.
		since time.Time
	}{
		{
			name: "an agent at work is not in triage",
			item: live(panel.StateThinking),
			want: "",
		},
		{
			name: "a prompt in a pane that has gone is not in triage",
			item: panel.AgentRow{Agent: panel.Agent{ID: "a1", State: panel.StateWaiting}},
			want: "",
		},
		{
			name: "a paused agent is not in triage",
			item: func() panel.AgentRow {
				item := live(panel.StateWaiting)
				item.Agent.HeldAt = xptr.Ptr(changed)

				return item
			}(),
			want: "",
		},
		{
			name: "a hidden agent is not in triage",
			item: func() panel.AgentRow {
				item := live(panel.StateWaiting)
				item.Agent.Hidden = true

				return item
			}(),
			want: "",
		},
		{
			name: "a conflict an agent is working through is not in triage",
			item: func() panel.AgentRow {
				item := live(panel.StateThinking)
				item.Agent.Sequence = panel.SequenceRebase

				return item
			}(),
			want: "",
		},
		{
			name: "a child in its parent's conflicted checkout is not in triage",
			item: idle(func(item *panel.AgentRow) {
				item.Agent.Sequence = panel.SequenceRebase
				item.Agent.ParentAgentID = xptr.Ptr("p1")
			}),
			want: "",
		},
		{
			name: "an agent holding mail is not in triage for what it can fix",
			item: idle(func(item *panel.AgentRow) {
				item.PullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING
				item.UnreadMail = true
			}),
			want: "",
		},
		{
			name: "a pull request opened with its checks still running is not in triage",
			item: idle(func(item *panel.AgentRow) {
				item.PullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
				item.PullRequest.Checks = pullrequestv1.Checks_CHECKS_PENDING
			}),
			want: "",
		},
		{
			name: "a child's pull request is not in triage",
			item: idle(func(item *panel.AgentRow) {
				item.PullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING
				item.Agent.ParentAgentID = xptr.Ptr("p1")
			}),
			want: "",
		},
		{
			name:   "an idle implementer with a child at work is not in triage",
			item:   implementer(func(*panel.AgentRow) {}),
			others: []panel.AgentRow{child(panel.StateThinking)},
			want:   "",
		},
		{
			name: "an idle child is not in triage",
			item: implementer(func(item *panel.AgentRow) { item.Agent.ParentAgentID = xptr.Ptr("p1") }),
			want: "",
		},
		{
			name: "an idle reviewer is not in triage",
			item: idle(func(item *panel.AgentRow) { item.Agent.Role = panel.RoleReviewer }),
			want: "",
		},
		{
			name: "an agent komrad cannot read and was not in triage stays out",
			item: live(panel.StateUncertain),
			want: "",
		},
		{
			name:  "an agent komrad cannot read stays in",
			was:   map[string]triageEntry{"a1": {since: earlier, rule: findRule("asking")}},
			item:  live(panel.StateUncertain),
			want:  "asking",
			since: earlier,
		},
		{
			name:  "an agent that stays in its tier keeps when it entered triage",
			was:   map[string]triageEntry{"a1": {since: earlier, rule: findRule("errored")}},
			item:  live(panel.StateFailedToCreate),
			want:  "launch failed",
			since: earlier,
		},
		{
			name:  "an agent that moves tier enters triage again",
			was:   map[string]triageEntry{"a1": {since: earlier, rule: findRule("asking")}},
			item:  live(panel.StateErrored),
			want:  "errored",
			since: changed,
		},
		{
			name: "the most urgent tier names the agent",
			item: func() panel.AgentRow {
				item := live(panel.StateWaiting)
				item.PullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING

				return item
			}(),
			want:  "asking",
			since: changed,
		},
		{
			name: "mail does not clear what only a person can",
			item: idle(func(item *panel.AgentRow) {
				item.FailedRun = true
				item.UnreadMail = true
			}),
			want:  "script failed",
			since: seen,
		},
		{
			name:  "a prompt is asking",
			item:  live(panel.StateWaiting),
			want:  "asking",
			since: changed,
		},
		{
			name:  "a conflict left for a person",
			item:  idle(func(item *panel.AgentRow) { item.Agent.Sequence = panel.SequenceRebase }),
			want:  "checkout conflict",
			since: seen,
		},
		{
			name: "a failed turn has errored",
			item: panel.AgentRow{Agent: panel.Agent{
				ID:             "a1",
				State:          panel.StateErrored,
				StateChangedAt: changed,
			}},
			want:  "errored",
			since: changed,
		},
		{
			name:  "a launch that failed",
			item:  live(panel.StateFailedToCreate),
			want:  "launch failed",
			since: changed,
		},
		{
			name:  "a cleanup that failed",
			item:  live(panel.StateFailedToCleanup),
			want:  "cleanup failed",
			since: changed,
		},
		{
			name:  "a script that failed",
			item:  idle(func(item *panel.AgentRow) { item.FailedRun = true }),
			want:  "script failed",
			since: seen,
		},
		{
			name:  "a pull request failing its checks",
			item:  pullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING),
			want:  "ci failing",
			since: seen,
		},
		{
			name:  "a pull request in conflict",
			item:  pullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING),
			want:  "pr conflict",
			since: seen,
		},
		{
			name:  "a pull request ready to merge",
			item:  pullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED),
			want:  "ready to merge",
			since: seen,
		},
		{
			name: "a pull request passing its checks is ready for review",
			item: idle(func(item *panel.AgentRow) {
				item.PullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
				item.PullRequest.Checks = pullrequestv1.Checks_CHECKS_PASSING
			}),
			want:  "ready for review",
			since: seen,
		},
		{
			name:  "an approved pull request behind its base",
			item:  pullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING),
			want:  "approved, behind",
			since: seen,
		},
		{
			name:  "a pull request with changes requested",
			item:  pullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED),
			want:  "changes requested",
			since: seen,
		},
		{
			name:  "comments a person left that are still open",
			item:  idle(func(item *panel.AgentRow) { item.PersonComments = &agentv1.CommentCounts{Open: 2} }),
			want:  "unanswered comments",
			since: seen,
		},
		{
			name:  "a merged pull request is done",
			item:  pullRequest(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED),
			want:  "done",
			since: seen,
		},
		{
			name:   "an implementer that finished its turn is idle",
			item:   implementer(func(*panel.AgentRow) {}),
			others: []panel.AgentRow{child(panel.StateIdle)},
			want:   "idle",
			since:  changed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone := triageZone{capacity: 1, entries: tt.was}

			zone.refresh(append([]panel.AgentRow{tt.item}, tt.others...), seen)

			entry, queued := zone.entries["a1"]
			assert.Equal(t, tt.want, entry.rule.name)
			assert.Equal(t, tt.want != "", queued)
			if queued {
				assert.Equal(t, tt.since, entry.since)
			}
		})
	}
}

// TestTriageRulesOrder covers the rules running from the most urgent tier down.
func TestTriageRulesOrder(t *testing.T) {
	assert.True(t, slices.IsSortedFunc(triageRules, func(a, b triageRule) int { return cmp.Compare(a.tier, b.tier) }))
}

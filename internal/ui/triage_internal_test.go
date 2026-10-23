package ui

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
)

// TestTriageRefresh covers which agents need triage, for what reason and since when.
func TestTriageRefresh(t *testing.T) {
	changed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	earlier := changed.Add(-time.Hour)
	seen := changed.Add(time.Hour)
	live := func(state agentv1.AgentState) panel.AgentRow {
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
		item := live(agentv1.AgentState_AGENT_STATE_IDLE)
		change(&item)

		return item
	}
	writer := func(change func(*panel.AgentRow)) panel.AgentRow {
		return idle(func(item *panel.AgentRow) {
			item.Agent.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
			change(item)
		})
	}
	reviewer := func(state agentv1.AgentState) panel.AgentRow {
		return panel.AgentRow{
			Agent: panel.Agent{
				ID:     "r1",
				State:  state,
				Role:   agentv1.AgentRole_AGENT_ROLE_REVIEWER,
				TaskID: "t1",
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
		// task is the task the agent works on.
		task panel.Task
		// others are the rest of the listing beside the agent.
		others []panel.AgentRow
		want   string
		// since is when the agent entered triage, unchecked when it is not.
		since time.Time
	}{
		{
			name: "an agent at work is not in triage",
			item: live(agentv1.AgentState_AGENT_STATE_THINKING),
			want: "",
		},
		{
			name: "a prompt in a pane that has gone is not in triage",
			item: panel.AgentRow{Agent: panel.Agent{ID: "a1", State: agentv1.AgentState_AGENT_STATE_WAITING}},
			want: "",
		},
		{
			name: "an agent on a hidden task is not in triage",
			item: func() panel.AgentRow {
				item := live(agentv1.AgentState_AGENT_STATE_WAITING)
				item.Agent.TaskID = "hidden"

				return item
			}(),
			want: "",
		},
		{
			name: "a conflict an agent is working through is not in triage",
			item: live(agentv1.AgentState_AGENT_STATE_THINKING),
			task: panel.Task{Sequence: taskv1.Sequence_SEQUENCE_REBASE},
			want: "",
		},
		{
			name: "a reviewer in its writer's conflicted checkout is not in triage",
			item: idle(func(item *panel.AgentRow) {
				item.Agent.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
			}),
			task: panel.Task{Sequence: taskv1.Sequence_SEQUENCE_REBASE},
			want: "",
		},
		{
			name: "an agent holding mail is not in triage for what it can fix",
			item: writer(func(item *panel.AgentRow) { item.UnreadMail = true }),
			want: "",
		},
		{
			name: "a writer waiting on its checks is not in triage",
			item: writer(func(*panel.AgentRow) {}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State:          pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
				MergeReadiness: panel.MergeReadiness{Checks: pullrequestv1.Checks_CHECKS_PENDING},
			}},
			want: "",
		},
		{
			name: "a writer with a failure while checks still run is not in triage",
			item: writer(func(*panel.AgentRow) {}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State:          pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
				MergeReadiness: panel.MergeReadiness{Checks: pullrequestv1.Checks_CHECKS_FAILING_RUNNING},
			}},
			want: "",
		},
		{
			name: "a writer whose reviewers were asked to look again is not in triage",
			item: writer(func(*panel.AgentRow) {}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED,
			}},
			want: "",
		},
		{
			name: "a writer whose pull request has changes requested is finished",
			item: writer(func(*panel.AgentRow) {}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
			}},
			want:  "finished",
			since: changed,
		},
		{
			name: "pending checks do not clear what only a person can",
			item: idle(func(item *panel.AgentRow) {
				item.FailedRun = true
			}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State:          pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
				MergeReadiness: panel.MergeReadiness{Checks: pullrequestv1.Checks_CHECKS_PENDING},
			}},
			want:  "script failed",
			since: seen,
		},
		{
			name:   "an idle writer with a reviewer at work is not in triage",
			item:   writer(func(*panel.AgentRow) {}),
			others: []panel.AgentRow{reviewer(agentv1.AgentState_AGENT_STATE_THINKING)},
			want:   "",
		},
		{
			name: "an idle reviewer is not in triage",
			item: idle(func(item *panel.AgentRow) { item.Agent.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER }),
			want: "",
		},
		{
			name: "an agent komrad cannot read and was not in triage stays out",
			item: live(agentv1.AgentState_AGENT_STATE_UNCERTAIN),
			want: "",
		},
		{
			name:  "an agent komrad cannot read stays in",
			was:   map[string]triageEntry{"a1": {since: earlier, rule: findRule("asking")}},
			item:  live(agentv1.AgentState_AGENT_STATE_UNCERTAIN),
			want:  "asking",
			since: earlier,
		},
		{
			name:  "an agent that stays in its tier keeps when it entered triage",
			was:   map[string]triageEntry{"a1": {since: earlier, rule: findRule("errored")}},
			item:  live(agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE),
			want:  "launch failed",
			since: earlier,
		},
		{
			name:  "an agent that moves tier enters triage again",
			was:   map[string]triageEntry{"a1": {since: earlier, rule: findRule("asking")}},
			item:  live(agentv1.AgentState_AGENT_STATE_ERRORED),
			want:  "errored",
			since: changed,
		},
		{
			name: "the most urgent tier names the agent",
			item: func() panel.AgentRow {
				item := live(agentv1.AgentState_AGENT_STATE_WAITING)
				item.FailedRun = true

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
			item:  live(agentv1.AgentState_AGENT_STATE_WAITING),
			want:  "asking",
			since: changed,
		},
		{
			name:  "a conflict left for a person",
			item:  writer(func(*panel.AgentRow) {}),
			task:  panel.Task{Sequence: taskv1.Sequence_SEQUENCE_REBASE},
			want:  "checkout conflict",
			since: seen,
		},
		{
			name: "a failed turn has errored",
			item: panel.AgentRow{Agent: panel.Agent{
				ID:             "a1",
				State:          agentv1.AgentState_AGENT_STATE_ERRORED,
				StateChangedAt: changed,
			}},
			want:  "errored",
			since: changed,
		},
		{
			name:  "a launch that failed",
			item:  live(agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE),
			want:  "launch failed",
			since: changed,
		},
		{
			name:  "a cleanup that failed",
			item:  live(agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP),
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
			name: "a writer that stopped on a failing pull request is finished",
			item: writer(func(*panel.AgentRow) {}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State: pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
			}},
			want:  "finished",
			since: changed,
		},
		{
			name: "a writer whose pull request merged with checks still running is finished",
			item: writer(func(*panel.AgentRow) {}),
			task: panel.Task{PullRequest: panel.PullRequestStanding{
				State:          pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				MergeReadiness: panel.MergeReadiness{Checks: pullrequestv1.Checks_CHECKS_PENDING},
			}},
			want:  "finished",
			since: changed,
		},
		{
			name:   "a writer that finished its turn is finished",
			item:   writer(func(*panel.AgentRow) {}),
			others: []panel.AgentRow{reviewer(agentv1.AgentState_AGENT_STATE_IDLE)},
			want:   "finished",
			since:  changed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone := triageZone{capacity: 1, entries: tt.was}

			item := tt.item
			if item.Agent.TaskID == "" {
				item.Agent.TaskID = "t1"
			}
			tasks := map[string]panel.Task{
				"hidden": {ID: "hidden", Hidden: true},
				"t1":     tt.task,
			}

			zone.refresh(append([]panel.AgentRow{item}, tt.others...), tasks, seen)

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

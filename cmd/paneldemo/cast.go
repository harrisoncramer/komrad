package main

import (
	"fmt"
	"time"

	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// cast is everything the fake service has to answer with, built once at startup.
type cast struct {
	agents         []*agentv1.Agent
	archivedAgents []*agentv1.Agent
	workflows      []*workflowv1.Workflow
	schedules      []*schedulev1.Schedule
	mail           []*agentv1.Mail
}

// agentSpec is one agent of the cast, written as the handful of things that decide how a row is
// drawn.
type agentSpec struct {
	name      string
	state     agentstore.State
	workflow  string
	unread    bool
	unviewed  bool
	mark      string
	branch    string
	failed    bool
	role      string
	parent    string
	reason    string
	hidden    bool
	scheduled bool
	archived  bool
}

// newCast builds a panel's worth of agents, workflows, schedules and mail, with enough variety
// that a screen drawn against it shows every row the panel draws differently.
func newCast() cast {
	workflows := []*workflowv1.Workflow{
		{
			Id: "wf-weave", Name: "weave itself", Cwd: "~/weave", Harness: "claude",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		},
		{
			Id: "wf-review", Name: "review flow", Cwd: "~/weave", Harness: "claude",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		},
		{Id: "wf-empty", Name: "nothing here yet", Cwd: "~/scratch", Harness: "codex"},
	}

	specs := []agentSpec{
		{name: "orca", state: agentstore.StateThinking, workflow: "wf-review", branch: "agent/orca"},
		{name: "narwhal", state: agentstore.StateIdle, workflow: "wf-review", unviewed: true, branch: "agent/narwhal"},
		{name: "pelican", state: agentstore.StateUncertain, workflow: "wf-review", reason: "the pane has not read since 11:02"},
		{name: "orca-reviewer", state: agentstore.StateThinking, workflow: "wf-review", role: "reviewer", parent: "agent-orca"},
		{name: "cowrie", state: agentstore.StateSettingUp, workflow: "wf-review"},
		{name: "mockup", state: agentstore.StateThinking, workflow: "wf-weave", unread: true, branch: "agent/mockup"},
		{name: "krill", state: agentstore.StateWaiting, workflow: "wf-weave", mark: "!", branch: "agent/krill", scheduled: true},
		{name: "limpet", state: agentstore.StateIdle, workflow: "wf-weave", failed: true},
		{name: "barnacle", state: agentstore.StateIdle, workflow: "wf-weave", unviewed: true, mark: "?", hidden: true},
		{
			name: "abalone", state: agentstore.StateFailedToCreate, workflow: "wf-weave",
			reason: "the worktree could not be made: fatal: 'agent/abalone' is already checked out at /Users/harry/weave-test",
		},
	}

	agents := make([]*agentv1.Agent, 0, len(specs))
	for i, spec := range specs {
		agents = append(agents, agentFrom(spec, i))
	}

	archived := []*agentv1.Agent{
		agentFrom(agentSpec{name: "urchin", state: agentstore.StateIdle, workflow: "wf-weave", archived: true}, 90),
		agentFrom(agentSpec{
			name: "cuttlefish", state: agentstore.StateFailedToCleanup, workflow: "wf-review", archived: true,
			reason: "the worktree is still there: unlinking failed, directory not empty",
		}, 91),
	}

	return cast{
		agents:         agents,
		archivedAgents: archived,
		workflows:      workflows,
		schedules:      newSchedules(),
		mail:           newMail(),
	}
}

func agentFrom(spec agentSpec, at int) *agentv1.Agent {
	agent := &agentv1.Agent{
		Id:             fmt.Sprintf("agent-%s", spec.name),
		Name:           spec.name,
		Cwd:            "~/.local/state/weave/worktrees/" + spec.name,
		Prompt:         "read the review and settle every thread it raises",
		PaneId:         fmt.Sprintf("%%%d", at+10),
		State:          string(spec.state),
		SessionId:      fmt.Sprintf("session-%s", spec.name),
		Harness:        "claude",
		WorkflowId:     spec.workflow,
		CreatedAt:      ago(time.Duration(at+1) * 37 * time.Minute),
		StateChangedAt: ago(time.Duration(at+1) * 4 * time.Minute),
		Alive:          !spec.archived,
		UnreadMail:     spec.unread,
		Unviewed:       spec.unviewed,
		FailedRun:      spec.failed,
		Role:           spec.role,
		ReportedReason: spec.reason,
		Hidden:         spec.hidden,
	}

	if spec.archived {
		agent.ArchivedAt = ptr(ago(26 * time.Hour))
	}

	if spec.scheduled {
		agent.ScheduleId = ptr("sched-triage")
	}

	if spec.mark != "" {
		agent.Mark = ptr(spec.mark)
	}

	if spec.branch != "" {
		agent.Branch = ptr(spec.branch)
		agent.Metadata = []*agentv1.AgentMetadata{
			{Key: "branch", Value: spec.branch},
			{Key: "summary", Value: "settled the review and opened the pull request"},
		}
	}

	if spec.parent != "" {
		agent.ParentAgentId = ptr(spec.parent)
		agent.SpawnedByAgentId = ptr(spec.parent)
	}

	return agent
}

func newSchedules() []*schedulev1.Schedule {
	return []*schedulev1.Schedule{
		{
			Id:                "sched-triage",
			Name:              "morning triage",
			Cron:              "0 7 * * 1-5",
			Timezone:          "America/New_York",
			Enabled:           true,
			Prompt:            "read the overnight alerts and write up anything that needs a human",
			WorkflowId:        "wf-weave",
			NextRunAt:         timestamppb.New(time.Now().Add(19 * time.Hour)),
			LastResultStatus:  "ok",
			LastResultMessage: "finished in 4m12s",
			LastResultAt:      timestamppb.New(time.Now().Add(-5 * time.Hour)),
		},
		{
			Id:                "sched-sweep",
			Name:              "nightly sweep",
			Cron:              "0 2 * * *",
			Timezone:          "America/New_York",
			Enabled:           false,
			Prompt:            "archive every agent whose pull request merged",
			WorkflowId:        "wf-weave",
			LastResultStatus:  "error",
			LastResultMessage: "no room for another agent",
			LastResultAt:      timestamppb.New(time.Now().Add(-50 * time.Hour)),
		},
		{
			Id:         "sched-digest",
			Name:       "weekly digest",
			Cron:       "0 9 * * 1",
			Timezone:   "America/New_York",
			Enabled:    true,
			Prompt:     "summarise what every agent landed this week",
			WorkflowId: "wf-review",
			NextRunAt:  timestamppb.New(time.Now().Add(72 * time.Hour)),
		},
	}
}

// newMail is a mailbox in id order, since real ids are uuidv7 and the mail screen takes id order
// for time order.
func newMail() []*agentv1.Mail {
	return []*agentv1.Mail{
		{
			Id:          "mail-1",
			FromAgentId: ptr("agent-orca"),
			FromName:    "orca",
			ToAgentId:   "agent-narwhal",
			Body:        "Taking the review screen. Leave reviewband.go alone so we do not collide.",
			CreatedAt:   ago(3 * time.Hour),
			ClaimedAt:   ptr(ago(3 * time.Hour)),
			ExpiresAt:   in(30 * time.Minute),
			Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		},
		{
			Id:          "mail-2",
			FromAgentId: ptr("agent-krill"),
			FromName:    "krill",
			ToAgentId:   "agent-mockup",
			Body:        "The migration landed on main, so rebase before you push. It renames the column your query reads.",
			CreatedAt:   ago(2 * time.Hour),
			ClaimedAt:   ptr(ago(105 * time.Minute)),
			ExpiresAt:   in(time.Hour),
			Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		},
		{
			Id:          "mail-3",
			FromAgentId: ptr("agent-mockup"),
			FromName:    "mockup",
			ToAgentId:   "agent-krill",
			Body:        "Rebased, and the query reads the new name. Thanks for the heads up.",
			CreatedAt:   ago(94 * time.Minute),
			ClaimedAt:   ptr(ago(90 * time.Minute)),
			ReplyTo:     ptr("mail-2"),
			ExpiresAt:   in(90 * time.Minute),
			Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		},
		{
			Id:        "mail-4",
			FromName:  "weave",
			ToAgentId: "agent-mockup",
			Body:      "review of round 3, approved, 0 comments",
			CreatedAt: ago(21 * time.Minute),
			ExpiresAt: in(2 * time.Hour),
			Kind:      agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY,
		},
		{
			Id:          "mail-5",
			FromAgentId: ptr("agent-mockup"),
			FromName:    "mockup",
			ToAgentId:   "agent-orca",
			Body:        "The pull request is open. Approved on round three, and the full check passed.",
			CreatedAt:   ago(7 * time.Minute),
			ExpiresAt:   in(3 * time.Hour),
			Kind:        agentv1.MailKind_MAIL_KIND_PLAIN,
		},
	}
}

// ago is a timestamp the given duration in the past, in the format the service sends.
func ago(d time.Duration) string {
	return time.Now().Add(-d).Format(time.RFC3339)
}

// in is a timestamp the given duration from now, which is what an expiry is.
func in(d time.Duration) string {
	return time.Now().Add(d).Format(time.RFC3339)
}

func ptr[T any](value T) *T { return &value }

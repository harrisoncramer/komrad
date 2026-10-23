package paneltest

import (
	"time"

	"komrad/internal/glyph"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ArchivedAt is when the archived rows were archived.
var ArchivedAt = timestamppb.New(time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC))

// Workflow is a live workflow row.
func Workflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:   id,
		Name: name,
	}
}

// ArchivedWorkflow is a workflow row archived at ArchivedAt.
func ArchivedWorkflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:         id,
		Name:       name,
		ArchivedAt: ArchivedAt,
	}
}

// Agent is an idle agent row in the workflow given.
func Agent(id, name, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       name,
		WorkflowId: workflowID,
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
	}
}

// AgentAt is an idle agent row keyed by its name, created the given minute past noon on 2026-09-20.
func AgentAt(name, workflowID string, minute int) *agentv1.Agent {
	agent := Agent(name, name, workflowID)
	agent.CreatedAt = timestamppb.New(time.Date(2026, 9, 20, 12, minute, 0, 0, time.UTC))

	return agent
}

// ArchivedAgent is an idle agent row in the workflow given, archived at ArchivedAt.
func ArchivedAgent(id, name, workflowID string) *agentv1.Agent {
	agent := Agent(id, name, workflowID)
	agent.ArchivedAt = ArchivedAt

	return agent
}

// Dormant is agent a1 in workflow w1, idle with no live session.
func Dormant(name string) *agentv1.Agent {
	agent := Agent("a1", name, "w1")
	agent.Alive = xptr.Ptr(false)

	return agent
}

// Running is agent a1, named old, alive in pane %9 in workflow w1.
func Running() *agentv1.Agent {
	agent := Agent("a1", "old", "w1")
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

// TaskOf makes an agent the writer of a task of its own and returns the task, for a test to give its checkout.
func TaskOf(agent *agentv1.Agent) *taskv1.Task {
	agent.TaskId = xptr.Ptr("task-" + agent.GetId())
	agent.Role = agentv1.AgentRole_AGENT_ROLE_WRITER

	return &taskv1.Task{
		Id:         agent.GetTaskId(),
		WorkflowId: agent.GetWorkflowId(),
		Slug:       "task-" + agent.GetId(),
	}
}

// Reviewer is an agent row reviewing on the task of the writer given, in workflow w1.
func Reviewer(id, name, writerID string) *agentv1.Agent {
	agent := Agent(id, name, "w1")
	agent.TaskId = xptr.Ptr("task-" + writerID)
	agent.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

	return agent
}

// Orchestrator is workflow w1's orchestrator row, alive and thinking in pane %9.
func Orchestrator() *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "o1",
		Name:       "orchestrator",
		WorkflowId: "w1",
		PaneId:     "%9",
		Harness:    "claude",
		Alive:      xptr.Ptr(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
		Role:       agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
	}
}

// RepositoryURL is the repository the pull request rows are opened on.
const RepositoryURL = "https://github.com/harrisoncramer/komrad"

// ReviewerMark is the mark rendered before a reviewer's name.
var ReviewerMark = glyph.Defaults().Spell(glyph.Reviewer) + " "

// ArchivedAgentAt is an idle agent row keyed by its name in workflow w1, archived at the time given.
func ArchivedAgentAt(name string, at time.Time) *agentv1.Agent {
	agent := ArchivedAgent(name, name, "w1")
	agent.ArchivedAt = dates.Stamp(at)

	return agent
}

// CreatedAgent is agent a2, named check-ci, alive in pane %9 in workflow w1.
func CreatedAgent() *agentv1.Agent {
	agent := Agent("a2", "check-ci", "w1")
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

// Thinking is agent a1, named telescope, alive and thinking in pane %9 in workflow w1.
func Thinking() *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "a1",
		Name:       "telescope",
		WorkflowId: "w1",
		PaneId:     "%9",
		Alive:      xptr.Ptr(true),
		State:      agentv1.AgentState_AGENT_STATE_THINKING,
	}
}

// UnreadMail is mail m1 to the agent given, expiring an hour from now.
func UnreadMail(agentID string) *agentv1.Mail {
	return &agentv1.Mail{
		Id:        "m1",
		ToAgentId: agentID,
		ExpiresAt: timestamppb.New(time.Now().Add(time.Hour).UTC()),
	}
}

// TriageAgent is an agent row in workflow w1, alive in the state given since the minute given past 1pm.
func TriageAgent(name string, state agentv1.AgentState, minute int) *agentv1.Agent {
	agent := AgentAt(name, "w1", minute)
	agent.State = state
	agent.Alive = xptr.Ptr(true)
	agent.StateChangedAt = timestamppb.New(time.Date(2026, 9, 20, 13, minute, 0, 0, time.UTC))

	return agent
}

// BuildTriage is a workflow with three agents waiting on a person and one that is not, broken the
// longest but in a tier below the others.
func BuildTriage() []proto.Message {
	return []proto.Message{
		Workflow("w1", "backend"),
		AgentAt("quiet", "w1", 1),
		TriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		TriageAgent("broken", agentv1.AgentState_AGENT_STATE_ERRORED, 2),
		TriageAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

// BuildSortable is two workflows whose agents sort differently by name, creation and activity.
func BuildSortable() []proto.Message {
	newSortable := func(name, workflowID string, created, changed int) *agentv1.Agent {
		agent := AgentAt(name, workflowID, created)
		agent.StateChangedAt = timestamppb.New(time.Date(2026, 9, 20, 12, changed, 0, 0, time.UTC))

		return agent
	}
	charlie := newSortable("charlie", "w1", 25, 5)
	charlie.TaskId = xptr.Ptr("task-alpha")
	charlie.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
	bob := newSortable("bob", "w1", 5, 40)
	bob.TaskId = xptr.Ptr("task-alpha")
	bob.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
	zulu := newSortable("zulu", "w1", 20, 30)
	zulu.Name = "Zulu"
	alpha := newSortable("alpha", "w1", 10, 20)

	return []proto.Message{
		Workflow("w1", "komrad"),
		Workflow("w2", "gadgets"),
		newSortable("delta", "w1", 30, 10),
		charlie,
		zulu,
		alpha,
		TaskOf(alpha),
		bob,
		newSortable("echo", "w2", 0, 50),
	}
}

// BuildRecorded is workflow backend with worktrees on and one agent, auth, with a directory and a session.
func BuildRecorded() []proto.Message {
	agent := AgentAt("auth", "w1", 0)
	agent.Id = "a1"
	agent.Cwd = "/repo"
	agent.SessionId = "abc"

	return []proto.Message{
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		agent,
	}
}

// BuildSelectable is two workflows of agents to move over, and an agent on a hidden task.
func BuildSelectable() []proto.Message {
	sweep := Agent("sweep", "sweep", "w1")
	hidden := TaskOf(sweep)
	hidden.Hidden = true

	return []proto.Message{
		Workflow("w1", "backend"),
		Workflow("w2", "frontend"),
		AgentAt("auth", "w1", 5),
		AgentAt("api", "w1", 4),
		AgentAt("db", "w1", 3),
		AgentAt("ui", "w2", 1),
		sweep,
		hidden,
	}
}

// FormWorkflow is workflow w1, named api, with a harness, a directory and flags.
func FormWorkflow() *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:      "w1",
		Name:    "api",
		Harness: "claude",
		Cwd:     "/work/api",
		Flags:   "--model opus",
	}
}

// ConfiguredWorkflow is FormWorkflow with a context and worktrees on.
func ConfiguredWorkflow() *workflowv1.Workflow {
	workflow := FormWorkflow()
	workflow.Context = "shared notes"
	workflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}

	return workflow
}

// FormAuthor is agent a1, named author, in workflow w1 with a directory, a harness and flags.
func FormAuthor() *agentv1.Agent {
	author := Agent("a1", "author", "w1")
	author.Cwd = "/work/api"
	author.Harness = "claude"
	author.Flags = "--model opus"

	return author
}

// LiveAuthor is agent a1, named author, a writer alive in pane %9 in workflow w1.
func LiveAuthor() *agentv1.Agent {
	author := Agent("a1", "author", "w1")
	author.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	author.PaneId = "%9"
	author.Alive = xptr.Ptr(true)

	return author
}

// BuildAuthorDocuments is the two documents the author wrote on its task, newest first.
func BuildAuthorDocuments() []*agentv1.Document {
	return []*agentv1.Document{
		{
			Id:        "d1",
			AgentId:   "author",
			TaskId:    xptr.Ptr("task-author"),
			Name:      "Design the cache",
			Type:      "RFC",
			Version:   3,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
		},
		{
			Id:        "d2",
			AgentId:   "author",
			TaskId:    xptr.Ptr("task-author"),
			Name:      "Rollout report",
			Type:      "report",
			Version:   5,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)),
		},
	}
}

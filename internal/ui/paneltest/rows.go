package paneltest

import (
	"time"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

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

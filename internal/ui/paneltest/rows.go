package paneltest

import (
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
)

// Workflow is a live workflow row.
func Workflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:   id,
		Name: name,
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

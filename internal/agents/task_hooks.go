package agents

import (
	"context"
	"strings"

	"komrad/config"
	"komrad/lib/xptr"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
)

// taskHookParams is one moment in a task's life and the agent whose action brought it about, nil when none did.
type taskHookParams struct {
	scripts  hookRunner
	workflow *workflowstore.Workflow
	task     taskstore.Task
	actor    *agentstore.Agent
	event    string
}

func runTaskPreHooks(ctx context.Context, params taskHookParams) error {
	return runPreHooks(ctx, params.hookRun())
}

func startTaskHooks(ctx context.Context, params taskHookParams) {
	startHooks(ctx, params.hookRun())
}

// hookRun runs a task's hooks as the acting agent, standing in the task's worktree while it has
// one and in its workflow's directory otherwise.
func (p taskHookParams) hookRun() hookRunParams {
	var runAs agentstore.Agent
	if p.actor != nil {
		runAs = *p.actor
	}
	runAs.WorkflowID = p.task.WorkflowID
	runAs.TaskID = &p.task.ID

	// An agent that provisions nothing is handed no worktree, which keeps the worktree's
	// variables from a script running in the workflow's own directory.
	if hasLiveCheckout(p.task) {
		runAs.Cwd = p.task.Checkout.Cwd
		runAs.ProvisionEnabled = nil
	} else {
		runAs.Cwd = workflowDir(p.workflow)
		runAs.ProvisionEnabled = xptr.Ptr(false)
	}

	var hooks []workflowstore.Hook
	if p.workflow != nil {
		hooks = p.workflow.Hooks
	}

	return hookRunParams{
		scripts: p.scripts,
		agent:   runAs,
		hooks:   hooks,
		event:   p.event,
	}
}

// hasLiveCheckout reports whether a task has a worktree that is still there.
func hasLiveCheckout(task taskstore.Task) bool {
	return task.Checkout.Cwd != "" && !task.Checkout.WorktreeRemoved
}

func workflowDir(wf *workflowstore.Workflow) string {
	if wf == nil {
		return ""
	}

	return config.ExpandHome(strings.TrimSpace(wf.Cwd))
}

// findTaskActor picks the agent among agents whose action fired a task's moment, preferring the
// task's writer and then any agent on it that is not a child.
func findTaskActor(agents []agentstore.Agent, taskID string) *agentstore.Agent {
	var actor *agentstore.Agent
	for _, agent := range agents {
		if agent.TaskID == nil || *agent.TaskID != taskID {
			continue
		}
		if actor == nil || actorRank(agent) > actorRank(*actor) {
			actor = &agent
		}
	}

	return actor
}

func actorRank(agent agentstore.Agent) int {
	switch {
	case agent.ParentAgentID != nil:
		return 0
	case agent.IsProvisionerDisabled():
		return 1
	default:
		return 2
	}
}

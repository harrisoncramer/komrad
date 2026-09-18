package ui

import (
	"slices"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// isReviewable reports whether an agent has a worktree of its own still on disk.
func (m model) isReviewable(agent agentstore.Agent) bool {
	if agent.Cwd == "" || agent.WorktreeRemoved {
		return false
	}

	index := slices.IndexFunc(m.listing.workflows, func(w workflowstore.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	return ownsWorktree(agent, m.listing.workflows[index])
}

// ownsWorktree reports whether a checkout is weave's to cut for this agent alone.
func ownsWorktree(agent agentstore.Agent, workflow workflowstore.Workflow) bool {
	// A child sits in its parent's checkout, and an agent told to keep the directory it was given
	// sits in somebody's own.
	if agent.ParentAgentID != nil || (agent.ProvisionEnabled != nil && !*agent.ProvisionEnabled) {
		return false
	}

	return workflow.Worktree.Enabled
}

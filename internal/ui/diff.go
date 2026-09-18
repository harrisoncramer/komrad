package ui

import (
	"slices"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// hasCheckout reports whether an agent's directory is still there to read.
func hasCheckout(agent agentstore.Agent) bool {
	return agent.Cwd != "" && !agent.WorktreeRemoved
}

// isReviewable reports whether an agent has work of its own to read.
func isReviewable(agent agentstore.Agent) bool {
	return hasCheckout(agent) && agent.ParentAgentID == nil
}

// isRebaseable reports whether an agent sits in a worktree weave cut for it alone, which is the
// only checkout weave moves onto a base.
func (m model) isRebaseable(agent agentstore.Agent) bool {
	if !hasCheckout(agent) {
		return false
	}

	index := slices.IndexFunc(m.listing.workflows, func(w workflowstore.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	// A child sits in its parent's checkout, and an agent told to keep the directory it was given
	// sits in somebody's own.
	if agent.ParentAgentID != nil || (agent.ProvisionEnabled != nil && !*agent.ProvisionEnabled) {
		return false
	}

	return m.listing.workflows[index].Worktree.Enabled
}

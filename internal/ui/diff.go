package ui

import (
	"slices"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// isReviewable reports whether an agent has a worktree of its own still on disk, since an agent
// sharing the user's checkout has no base branch to measure against.
func (m model) isReviewable(agent agentstore.Agent) bool {
	if agent.Cwd == "" || agent.WorktreeRemoved {
		return false
	}
	// A child sits in its parent's checkout, and an agent told to keep the directory it was given
	// sits in somebody's own, so the diff there is that person's work and the review threads on
	// it belong on their row rather than this one.
	if agent.ParentAgentID != nil || (agent.ProvisionEnabled != nil && !*agent.ProvisionEnabled) {
		return false
	}

	index := slices.IndexFunc(m.listing.workflows, func(w workflowstore.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	return m.listing.workflows[index].Worktree.Enabled
}

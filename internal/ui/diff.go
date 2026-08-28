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

	index := slices.IndexFunc(m.listing.workflows, func(w workflowstore.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	return m.listing.workflows[index].Worktree.Enabled
}

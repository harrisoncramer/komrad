package ui

import (
	"slices"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// isReviewable reports whether an agent has a diff worth reviewing, which means a worktree of
// its own that is still on disk. An agent sharing the user's checkout has no base branch to
// be measured against.
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

package ui

import "komrad/internal/ui/screen"

// archivedSaid is why a write on an archived agent's review did nothing.
const archivedSaid = "this agent is archived" + separated + "restore it before writing to it"

// worktreeRemovedSaid is why the editor opens nothing on a task whose worktree was removed.
const worktreeRemovedSaid = "the worktree was removed, so there is no checkout to edit"

// isWorktreeRemoved reports whether the worktree of the task on screen was removed.
func (r reviewScreen) isWorktreeRemoved(in screen.Inputs) bool {
	return in.Listing.Tasks[r.taskID].IsWorktreeRemoved()
}

// isArchived reports whether the agent has left the listing and can take no writes.
func (r reviewScreen) isArchived(in screen.Inputs) bool {
	_, ok := r.agentRow(in)

	return !ok
}

// isHistory reports whether the diff on screen ends before the snapshot new comments go on.
func (r reviewScreen) isHistory() bool {
	return r.diff.View() != nil && r.diff.isHistory()
}

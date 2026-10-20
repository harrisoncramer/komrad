package ui

import (
	agentv1 "komrad/proto/gen/agent/v1"
)

// unsentCount is how many comments the reader is holding.
func (t reviewThreads) unsentCount() int {
	return t.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)]
}

// archivedSaid is why a write on an archived agent's review did nothing.
const archivedSaid = "this agent is archived" + separated + "restore it before writing to it"

// worktreeRemovedSaid is why the editor opens nothing on a task whose worktree was removed.
const worktreeRemovedSaid = "the worktree was removed, so there is no checkout to edit"

// isWorktreeRemoved reports whether the worktree of the task on screen was removed.
func (r reviewScreen) isWorktreeRemoved(m *model) bool {
	return m.listing.tasks[r.taskID].WorktreeRemoved
}

// isArchived reports whether the agent has left the listing and can take no writes.
func (r reviewScreen) isArchived(m *model) bool {
	_, ok := r.agentRow(m)

	return !ok
}

// holdsUnsent says whether the reader has comments nobody has seen.
func (t reviewThreads) holdsUnsent() bool {
	return t.unsentCount() > 0
}

// isHistory reports whether the diff on screen ends before the snapshot new comments go on.
func (r reviewScreen) isHistory() bool {
	return r.diff.View() != nil && r.diff.isHistory()
}

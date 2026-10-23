package review

import "komrad/internal/ui/screen"

// isWorktreeMoved reports whether the agent has written since the snapshot on screen.
func (r Screen) isWorktreeMoved(in screen.Inputs) bool {
	return r.findMovedSnapshot(in) != ""
}

// findMovedSnapshot is the snapshot on screen when the agent has moved past it, empty otherwise.
func (r Screen) findMovedSnapshot(in screen.Inputs) string {
	item, ok := r.agentRow(in)
	if !ok {
		return ""
	}

	moved := in.Listing.Tasks.FindForAgent(item.Agent).MovedSinceSnapshot
	if moved == "" || moved != r.snapshot.GetSha() {
		return ""
	}

	return moved
}

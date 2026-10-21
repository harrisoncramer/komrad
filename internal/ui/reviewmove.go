package ui

// isWorktreeMoved reports whether the agent has written since the snapshot on screen.
func (r reviewScreen) isWorktreeMoved(m *model) bool {
	return r.findMovedSnapshot(m) != ""
}

// findMovedSnapshot is the snapshot on screen when the agent has moved past it, empty otherwise.
func (r reviewScreen) findMovedSnapshot(m *model) string {
	item, ok := r.agentRow(m)
	if !ok {
		return ""
	}

	moved := m.listing.tasks.findForAgent(item.Agent).MovedSinceSnapshot
	if moved == "" || moved != r.snapshot.GetSha() {
		return ""
	}

	return moved
}

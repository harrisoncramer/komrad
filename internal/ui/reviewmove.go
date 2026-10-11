package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// followMove reads the diff again when the watch says the worktree has moved past the round on
// screen.
func (r *reviewScreen) followMove(m *model) tea.Cmd {
	moved := r.findMovedRound(m)
	if moved == "" {
		r.diff.followedMove = ""

		return nil
	}

	if moved == r.diff.followedMove || r.takesEveryKey() || !r.doesFollowLive(m) {
		return nil
	}

	return r.reload(m)
}

// isWorktreeMoved reports whether the agent has written since the round on screen was taken.
func (r reviewScreen) isWorktreeMoved(m *model) bool {
	return r.findMovedRound(m) != ""
}

// findMovedRound is the snapshot on screen when the agent has moved past it, empty otherwise.
func (r reviewScreen) findMovedRound(m *model) string {
	item, ok := r.agentRow(m)
	if !ok {
		return ""
	}

	moved := m.listing.findTask(item.Agent).MovedSinceRound
	if moved == "" || moved != r.round.GetSnapshotSha() {
		return ""
	}

	return moved
}

// doesFollowLive reports whether the diff on screen is meant to keep up with the agent.
func (r reviewScreen) doesFollowLive(m *model) bool {
	if r.hold.isHeld() || r.diff.View() == nil || r.loading {
		return false
	}

	if r.isAgentWriting(m) {
		return false
	}

	return !r.threads.holdsUnsent() && !r.diff.spec.isChapter() && !r.diff.pinned
}

// isAgentWriting reports whether the agent whose diff is on screen is putting words in its
// worktree.
func (r reviewScreen) isAgentWriting(m *model) bool {
	item, ok := r.agentRow(m)

	return ok && item.Alive && item.Agent.State == panel.StateThinking
}

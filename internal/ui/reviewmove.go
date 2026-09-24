package ui

import (
	"weave/internal/ui/panel"

	tea "github.com/charmbracelet/bubbletea"
)

// followMove reads the diff again when the watch says the worktree has moved past the round on
// screen.
func (r *reviewScreen) followMove(s panelShared) tea.Cmd {
	moved := r.findMovedRound(s)
	if moved == "" {
		r.diff.followedMove = ""

		return nil
	}

	if moved == r.diff.followedMove || r.takesEveryKey() || !r.doesFollowLive(s) {
		return nil
	}

	r.diff.followedMove = moved

	return r.reload(s)
}

// isWorktreeMoved reports whether the agent has written since the round on screen was taken.
func (r reviewScreen) isWorktreeMoved(s panelShared) bool {
	return r.findMovedRound(s) != ""
}

// findMovedRound is the snapshot on screen when the agent has moved past it, empty otherwise.
func (r reviewScreen) findMovedRound(s panelShared) string {
	item, ok := r.agentRow(s)
	if !ok || item.MovedSinceRound == "" || item.MovedSinceRound != r.round.GetSnapshotSha() {
		return ""
	}

	return item.MovedSinceRound
}

// doesFollowLive reports whether the diff on screen is meant to keep up with the agent.
func (r reviewScreen) doesFollowLive(s panelShared) bool {
	if r.pass.mode != modeWatching || r.diff.view == nil || r.loading {
		return false
	}

	if r.isAgentWriting(s) {
		return false
	}

	return !r.threads.holdsUnsent() && r.diff.chapter < 0 && !r.diff.pinned
}

// isAgentWriting reports whether the agent whose diff is on screen is putting words in its
// worktree.
func (r reviewScreen) isAgentWriting(s panelShared) bool {
	item, ok := r.agentRow(s)

	return ok && item.Alive && item.Agent.State == panel.StateThinking
}

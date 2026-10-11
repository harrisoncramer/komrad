package ui

import (
	"context"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

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
	moved, _ := r.findTaskMove(m)
	if moved == "" || moved != r.round.GetSnapshotSha() {
		return ""
	}

	return moved
}

func (r reviewScreen) findTaskMove(m *model) (string, bool) {
	item, ok := r.agentRow(m)
	if !ok {
		return "", false
	}

	return m.listing.tasks.findForAgent(item.Agent).MovedSinceRound, true
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

type reviewRoundsReadMsg struct {
	agentID string
	moved   string
	rounds  []*agentv1.ReviewRound
	err     error
}

func (r *reviewScreen) followRounds(m *model) tea.Cmd {
	if !r.isFrozen() || r.loading {
		return nil
	}

	moved, ok := r.findTaskMove(m)
	if !ok || moved == r.diff.frozenRoundsMove || m.listing.generation == r.diff.frozenRoundsAsked {
		return nil
	}
	r.diff.frozenRoundsAsked = m.listing.generation

	agents, agentID, taskID := m.api.reads().agents, r.agentID, r.taskID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		rounds, err := listReviewRounds(ctx, agents, taskID)

		return reviewRoundsReadMsg{agentID: agentID, moved: moved, rounds: rounds, err: err}
	}
}

func (r *reviewScreen) applyRoundsRead(msg reviewRoundsReadMsg) {
	if msg.agentID != r.agentID || msg.err != nil {
		return
	}

	r.diff.frozenRoundsMove = msg.moved
	if len(msg.rounds) >= len(r.diff.rounds) {
		r.diff.rounds = msg.rounds
	}
}

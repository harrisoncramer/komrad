package ui

import (
	"context"
	"time"

	"weave/internal/ui/panel"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewWorktreeHashInterval is how often the agent's worktree is hashed while the review is up.
const reviewWorktreeHashInterval = 5 * time.Second

// reviewWorktree is what the review knows of the agent's worktree against the round on screen.
type reviewWorktree struct {
	// moved is set once the agent has written something since the round on screen was taken.
	moved bool
	// hashing is set while a hash of the worktree is in flight.
	hashing bool
	// hashStartedAt is when the last hash started.
	hashStartedAt time.Time
}

// reviewWorktreeHashedMsg is the worktree as a background hash found it.
type reviewWorktreeHashedMsg struct {
	agentID string
	// moved says the agent has written since the round on screen was taken.
	moved bool
	err   error
}

// isHashDue reports whether another hash may start.
func (w reviewWorktree) isHashDue(now time.Time) bool {
	return !w.hashing && now.Sub(w.hashStartedAt) >= reviewWorktreeHashInterval
}

// markHashStarted records a hash as in flight from now.
func (w *reviewWorktree) markHashStarted(now time.Time) {
	w.hashing = true
	w.hashStartedAt = now
}

// markCaughtUp records the diff on screen as matching the worktree.
func (w *reviewWorktree) markCaughtUp() {
	w.moved = false
}

// recordHash records a finished hash and reports whether it found the worktree moved.
func (w *reviewWorktree) recordHash(msg reviewWorktreeHashedMsg) bool {
	w.hashing = false

	// A failed hash leaves moved as it was.
	if msg.err != nil {
		return false
	}

	w.moved = msg.moved

	return w.moved
}

// hashWorktreeIfDue starts a hash of the agent's worktree when the last one is old enough.
func (r *reviewScreen) hashWorktreeIfDue(s panelShared, now time.Time) tea.Cmd {
	if r.diff.view == nil || r.takesEveryKey() || !r.diff.worktree.isHashDue(now) {
		return nil
	}

	r.diff.worktree.markHashStarted(now)

	return r.hashWorktreeCmd(s)
}

// hashWorktreeCmd hashes the agent's worktree against the snapshot of the round on screen.
func (r reviewScreen) hashWorktreeCmd(s panelShared) tea.Cmd {
	agentID, dir := r.agentID, r.dir
	snapshot := r.round.GetSnapshotSha()
	repo := s.git

	return func() tea.Msg {
		moved, err := call(reviewTimeout, func(ctx context.Context) (bool, error) {
			probe, err := repo.ProbeTree(ctx, dir)
			if err != nil {
				return false, err
			}

			held, err := repo.GetTree(ctx, dir, snapshot)
			if err != nil {
				return false, err
			}

			return probe != held, nil
		})
		if err != nil {
			return reviewWorktreeHashedMsg{agentID: agentID, err: err}
		}

		return reviewWorktreeHashedMsg{agentID: agentID, moved: moved}
	}
}

func (r *reviewScreen) applyWorktreeHashed(s panelShared, msg reviewWorktreeHashedMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	if !r.diff.worktree.recordHash(msg) || !r.doesFollowLive(s) {
		return nil
	}

	return r.reload(s)
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

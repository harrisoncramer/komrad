package ui

import (
	"context"
	"time"

	"weave/internal/ui/panel"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewTreeInterval is how often the worktree behind the diff is hashed while the review is up.
const reviewTreeInterval = 5 * time.Second

// reviewTree is what the review knows of the agent's worktree against the round on screen.
type reviewTree struct {
	// moved is set once the agent has written something since the round on screen was taken.
	moved bool
	// checking is set while a hash of the worktree is in flight.
	checking bool
	// checkedAt is when the last hash started.
	checkedAt time.Time
}

// reviewTreeCheckedMsg is the worktree as a background hash found it.
type reviewTreeCheckedMsg struct {
	agentID string
	// moved says the agent has written since the round on screen was taken.
	moved bool
	err   error
}

// isDue reports whether another hash may start.
func (t reviewTree) isDue(now time.Time) bool {
	return !t.checking && now.Sub(t.checkedAt) >= reviewTreeInterval
}

// land takes a finished hash and reports whether the worktree has moved.
func (t *reviewTree) land(msg reviewTreeCheckedMsg) bool {
	t.checking = false

	// A worktree emptied under an open review is a move like any other, so the bar offers to catch
	// up and the reload is what finds nothing to show and backs out to the listing.
	if msg.err != nil {
		return false
	}

	// A worktree edited back to the round it was cut from is not a move, so the notice goes
	// away rather than standing until the reader opens a round for nothing.
	t.moved = msg.moved

	return t.moved
}

// checkTree is what the panel's own tick asks the review for: a hash of the worktree every few
// seconds.
func (r *reviewScreen) checkTree(s panelShared, now time.Time) tea.Cmd {
	if r.diff.view == nil || r.takesEveryKey() || !r.diff.tree.isDue(now) {
		return nil
	}

	r.diff.tree.checking = true
	r.diff.tree.checkedAt = now

	return r.checkTreeCmd(s)
}

// checkTreeCmd asks whether the agent has written anything since the round on screen was taken,
// hashing the worktree rather than reading a second patch.
func (r reviewScreen) checkTreeCmd(s panelShared) tea.Cmd {
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
			return reviewTreeCheckedMsg{agentID: agentID, err: err}
		}

		return reviewTreeCheckedMsg{agentID: agentID, moved: moved}
	}
}

func (r *reviewScreen) applyTreeChecked(s panelShared, msg reviewTreeCheckedMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	if !r.diff.tree.land(msg) || !r.doesFollowLive(s) {
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

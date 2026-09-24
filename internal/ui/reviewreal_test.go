package ui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// TestReviewAgainstARealWorktree drives the screen over a real agent worktree.
func TestReviewAgainstARealWorktree(t *testing.T) {
	dir := os.Getenv("WEAVE_REVIEW_DIR")
	if dir == "" {
		t.Skip("set WEAVE_REVIEW_DIR to a worktree to run this")
	}

	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	// The real workflow names no base branch, so the loader has to ask the repository.
	m.listing.workflows[0].Worktree.BaseBranch = os.Getenv("WEAVE_REVIEW_BASE")
	m.width, m.height = 76, 68
	tmux.width = 304

	m = openReview(t, m)
	require.NotNil(t, m.review.diff.view)
	t.Logf("rows=%d", m.review.diff.view.Len())

	_ = m.review.view(m.shared())

	for _, key := range []tea.KeyPressMsg{
		keyMsg("j"),
		keyMsg("G"),
		keyMsg("pgdown"),
		keyMsg("pgup"),
		keyMsg("]"),
		keyMsg("["),
		keyMsg("l"),
		keyMsg("h"),
		keyMsg("t"),
		keyMsg("}"),
		keyMsg("{"),
		keyMsg("g"),
		keyMsg("v"),
		keyMsg("v"),
	} {
		next, _ := m.routeScreenMessage(key)
		m = next.(model)
		_ = m.review.view(m.shared())
	}

	t.Logf("respawns=%d last=%s", len(tmux.respawns), tmux.respawns[len(tmux.respawns)-1])
}

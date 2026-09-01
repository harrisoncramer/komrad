package ui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// TestReviewAgainstARealWorktree drives the screen over a real agent worktree, which is the
// only way to hit the shapes a synthetic patch does not have: renames, binaries, deletions,
// files with no trailing newline.
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
	require.NotNil(t, m.review.view)
	t.Logf("rows=%d", m.review.view.Len())

	_ = m.reviewView()

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyRunes, Runes: []rune("G")},
		{Type: tea.KeyPgDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyRunes, Runes: []rune("]")},
		{Type: tea.KeyRunes, Runes: []rune("[")},
		{Type: tea.KeyRunes, Runes: []rune("l")},
		{Type: tea.KeyRunes, Runes: []rune("h")},
		{Type: tea.KeyRunes, Runes: []rune("t")},
		{Type: tea.KeyRunes, Runes: []rune("}")},
		{Type: tea.KeyRunes, Runes: []rune("{")},
		{Type: tea.KeyRunes, Runes: []rune("g")},
		{Type: tea.KeyRunes, Runes: []rune("v")},
		{Type: tea.KeyRunes, Runes: []rune("v")},
	} {
		next, _ := m.updateReview(key)
		m = next.(model)
		_ = m.reviewView()
	}

	t.Logf("respawns=%d last=%s", len(tmux.respawns), tmux.respawns[len(tmux.respawns)-1])
}

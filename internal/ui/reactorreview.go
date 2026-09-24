package ui

import (
	"weave/internal/assemble"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewReactor reads the review's conversation again when the watch has seen it move.
type reviewReactor struct{}

func (reviewReactor) react(m *model, _ assemble.Changed) tea.Cmd {
	if m.screen != screenReview {
		return nil
	}

	return tea.Batch(m.review.followWatch(m.shared()), m.review.followMove(m.shared()))
}

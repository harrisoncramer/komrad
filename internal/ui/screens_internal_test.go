package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// resizeModel puts the terminal and the panel's pane at the same size.
func resizeModel(t *testing.T, m model, size tea.WindowSizeMsg) model {
	t.Helper()

	tmux, ok := m.tmux.(*recordingTmux)
	require.True(t, ok)
	tmux.width, tmux.height = size.Width, size.Height

	sized, _ := m.route(size)

	return requireModel(t, sized)
}

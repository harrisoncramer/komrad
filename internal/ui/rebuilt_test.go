package ui

import (
	"testing"
	"time"

	"weave/config"
	"weave/internal/errui"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBinary struct {
	replaced bool
	asked    int
}

func (b *fakeBinary) IsReplaced() bool {
	b.asked++

	return b.replaced
}

func rebuiltPanel(binary BinaryWatcher) (model, *recordingTmux) {
	tmux := &recordingTmux{sessionClient: panelViewer}

	m := model{
		frame:  layout.New(tmux, config.DefaultLayout()),
		popup:  NewPopupWindow(tmux, "test-session"),
		keys:   config.DefaultKeybindings(),
		binary: binary,
		width:  80,
		screen: screenAgents,
	}
	m.frame.Attach("%0")
	m.frame.ShowGuest("%9")

	return m, tmux
}

func tick(t *testing.T, m model) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(refreshTickMsg{at: time.Now()})

	ticked, ok := next.(model)
	require.True(t, ok)

	return ticked, cmd
}

// TestRebuiltPanelPutsUpTheStartupScreen covers the binary changing under a running panel,
// which gives the window back and takes over the screen the way a failed start does, so the
// next key closes it and the panel key opens one from the new build.
func TestRebuiltPanelPutsUpTheStartupScreen(t *testing.T) {
	binary := &fakeBinary{}
	m, tmux := rebuiltPanel(binary)

	m, cmd := tick(t, m)
	require.NotNil(t, cmd, "an unchanged binary leaves the refresh loop running")
	assert.NoError(t, m.fatal())
	assert.Equal(t, 1, binary.asked, "the file is looked at once per refresh")

	binary.replaced = true

	m, cmd = tick(t, m)
	assert.Nil(t, cmd, "the refresh loop stops")
	require.ErrorIs(t, m.fatal(), errui.ErrRebuilt)
	assert.Empty(t, m.frame.GuestPane(), "the agent's pane goes back to its own window")
	assert.Equal(t, 2, tmux.swaps)
	assert.Contains(t, m.View(), "weave was rebuilt")

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd(), "any key closes it")
	assert.Equal(t, 2, binary.asked, "a binary seen to change is not looked at again")
}

// TestPanelWithoutABinaryToWatchStaysUp covers a panel built without a watcher.
func TestPanelWithoutABinaryToWatchStaysUp(t *testing.T) {
	m, _ := rebuiltPanel(nil)

	m, cmd := tick(t, m)
	require.NotNil(t, cmd)
	assert.NoError(t, m.fatal())
}

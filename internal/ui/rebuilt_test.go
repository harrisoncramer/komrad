package ui

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"weave/config"
	"weave/internal/errui"
	"weave/internal/ui/layout"

	tea "charm.land/bubbletea/v2"
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

func ticked(t *testing.T, m model) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(tickMsg{at: time.Now()})

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

	m, cmd := ticked(t, m)
	require.NotNil(t, cmd, "an unchanged binary leaves the refresh loop running")
	require.NoError(t, m.fatal())
	assert.Equal(t, 1, binary.asked, "the file is looked at once per refresh")

	binary.replaced = true

	m, cmd = ticked(t, m)
	assert.Nil(t, cmd, "the refresh loop stops")
	require.ErrorIs(t, m.fatal(), errui.ErrRebuilt)
	assert.Empty(t, m.frame.GuestPane(), "the agent's pane goes back to its own window")
	assert.Equal(t, 2, tmux.swaps)
	assert.Contains(t, ansi.Strip(m.render()), "weave was rebuilt")

	_, cmd = m.Update(keyMsg(string('x')))
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd(), "any key closes it")
	assert.Equal(t, 2, binary.asked, "a binary seen to change is not looked at again")
}

// TestRebuiltPanelWaitsForWhatIsInFlight covers what keeps a rebuilt panel from taking the
// screen.
func TestRebuiltPanelWaitsForWhatIsInFlight(t *testing.T) {
	tests := []struct {
		name string
		busy func(m *model)
	}{
		{
			name: "a write in flight",
			busy: func(m *model) { m.pending = 1 },
		},
		{
			name: "a held form",
			busy: func(m *model) { m.held.active = true },
		},
		{
			name: "a review message being written",
			busy: func(m *model) { m.review.threads.composing = true },
		},
		{
			name: "a pass over an agent's work",
			busy: func(m *model) { m.review.pass.mode = modeReviewing },
		},
		{
			name: "an editor open on the stage",
			busy: func(m *model) { m.review.pass.atEditor = true },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := &fakeBinary{replaced: true}
			m, _ := rebuiltPanel(binary)
			tt.busy(&m)

			m, cmd := ticked(t, m)
			require.NotNil(t, cmd, "the refresh loop keeps running")
			require.NoError(t, m.fatal())
			assert.Equal(t, "%9", m.frame.GuestPane(), "the window is kept")
			assert.Zero(t, binary.asked, "the file is not looked at while the panel is busy")
		})
	}
}

// TestPanelWithoutABinaryToWatchStaysUp covers a panel built without a watcher.
func TestPanelWithoutABinaryToWatchStaysUp(t *testing.T) {
	m, _ := rebuiltPanel(nil)

	m, cmd := ticked(t, m)
	require.NotNil(t, cmd)
	assert.NoError(t, m.fatal())
}

package ui

import (
	"testing"
	"time"

	"weave/config"
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

func rebuiltPanel(binary *fakeBinary) (model, *recordingTmux) {
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

// TestRebuiltPanelSaysSoThenQuits covers the binary changing under a running panel, which is
// reported on the refresh that sees it and acted on one refresh later.
func TestRebuiltPanelSaysSoThenQuits(t *testing.T) {
	binary := &fakeBinary{}
	m, tmux := rebuiltPanel(binary)

	m, cmd := tick(t, m)
	require.NotNil(t, cmd)
	assert.Empty(t, m.messages.items, "an unchanged binary says nothing")
	assert.Equal(t, 1, binary.asked, "the file is looked at once per refresh")

	binary.replaced = true

	m, cmd = tick(t, m)
	require.NotNil(t, cmd)
	require.Len(t, m.messages.items, 1)
	assert.Equal(t, rebuiltSummary, m.messages.items[0].summary)
	assert.Equal(t, "%9", m.frame.GuestPane(), "the pass that reports it still holds the window")

	m, cmd = tick(t, m)
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd(), "the next pass quits")
	assert.Empty(t, m.frame.GuestPane(), "the agent's pane goes back to its own window first")
	assert.Equal(t, 2, tmux.swaps)
	assert.Equal(t, 2, binary.asked, "a binary seen to change is not looked at again")
}

// TestRebuiltPanelWaitsForWhatIsInFlight covers what keeps a rebuilt panel from quitting.
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
			m, _ := rebuiltPanel(&fakeBinary{replaced: true})
			m.rebuilt = true
			tt.busy(&m)

			m, cmd := tick(t, m)
			require.NotNil(t, cmd)
			assert.NotEqual(t, tea.Quit(), cmd())
			assert.Equal(t, "%9", m.frame.GuestPane(), "the window is kept while it is busy")
		})
	}
}

// TestPanelWithoutABinaryToWatchNeverQuits covers a panel built without a watcher.
func TestPanelWithoutABinaryToWatchNeverQuits(t *testing.T) {
	m, _ := rebuiltPanel(nil)
	m.binary = nil
	m.rebuilt = true

	_, cmd := tick(t, m)
	require.NotNil(t, cmd)
	assert.NotEqual(t, tea.Quit(), cmd())
}

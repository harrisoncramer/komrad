package layout

import (
	"testing"

	"weave/config"
	"weave/internal/engine"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// remoteEngine is an engine somewhere this tmux server cannot reach.
func remoteEngine() engine.Remote {
	return engine.Remote{
		Destination: "weave@host",
		Socket:      "weave",
		Session:     "weave",
	}
}

// TestFocusPaneIsTheGuestForALocalEngine covers the focus key jumping to the agent's own pane,
// which is the pane that was moved into the stage.
func TestFocusPaneIsTheGuestForALocalEngine(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%1")
	frame.ShowGuest("%7")

	assert.Equal(t, "%7", frame.FocusPane())
}

// TestFocusPaneIsTheStageForARemoteEngine covers an agent's pane belonging to another server,
// where the only pane the focus key can land on is the stage attached to it and without which
// the key is unbound.
func TestFocusPaneIsTheStageForARemoteEngine(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := New(tmux, config.DefaultLayout(), WithEngine(remoteEngine()))
	frame.Attach("%1")

	require.Empty(t, frame.FocusPane(), "nothing is staged yet")

	frame.ShowGuest("%7")

	stage := frame.Pane(RegionStage)
	require.NotEmpty(t, stage)
	assert.Equal(t, stage, frame.FocusPane())
	assert.NotEqual(t, "%7", frame.FocusPane(), "the agent's pane belongs to the engine")
}

// TestRemoteGuestIsNotTrackedAsAGuest covers the swap-back path never being handed a pane this
// server does not hold.
func TestRemoteGuestIsNotTrackedAsAGuest(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := New(tmux, config.DefaultLayout(), WithEngine(remoteEngine()))
	frame.Attach("%1")
	frame.ShowGuest("%7")

	assert.Empty(t, frame.GuestPane())
}

// TestFocusPaneIsEmptyOnceTheStageShowsSomethingElse covers the key being given back when the stage
// stops holding an agent.
func TestFocusPaneIsEmptyOnceTheStageShowsSomethingElse(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := New(tmux, config.DefaultLayout(), WithEngine(remoteEngine()))
	frame.Attach("%1")
	frame.ShowGuest("%7")
	require.NotEmpty(t, frame.FocusPane())

	frame.Show(RegionStage, Content{
		Key:    "blank",
		Render: func(Target) ([]string, bool) { return []string{"true"}, true },
	})

	assert.Empty(t, frame.FocusPane())
}

// TestRemoteGuestIsAttachedAgainOnceItsViewerDies covers the ssh in the stage dropping while the
// cursor stays on the same agent, which takes the stage pane with it.
func TestRemoteGuestIsAttachedAgainOnceItsViewerDies(t *testing.T) {
	tmux := &recordingTmux{
		width:     80,
		height:    40,
		deadPanes: map[string]bool{},
	}
	frame := New(tmux, config.DefaultLayout(), WithEngine(remoteEngine()))
	frame.Attach("%1")
	frame.ShowGuest("%7")

	stage := frame.Pane(RegionStage)
	require.NotEmpty(t, stage)
	require.Len(t, tmux.respawns, 1)

	frame.ShowGuest("%7")
	require.Len(t, tmux.respawns, 1, "the same agent is not attached twice")

	tmux.deadPanes[stage] = true

	frame.ShowGuest("%7")

	assert.Len(t, tmux.respawns, 2, "the viewer is run again in a fresh stage")
	assert.Equal(t, frame.Pane(RegionStage), frame.FocusPane())
}

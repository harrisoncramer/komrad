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
	return engine.Remote{Destination: "weave@host", Socket: "weave", Session: "weave"}
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

// TestFocusPaneIsTheStageForARemoteEngine covers the agent's pane belonging to another server,
// so the only pane the key can land on is the stage that is attached to it. Without this the
// key is unbound and pressing it does nothing.
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

// TestFocusPaneIsEmptyOnceTheStageShowsSomethingElse covers the key being given back when the
// stage stops holding an agent, which is what stops it stealing the key from the panes.
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

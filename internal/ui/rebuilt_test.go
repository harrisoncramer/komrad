package ui_test

import (
	"testing"
	"time"

	"weave/internal/ui"
	"weave/proto/gen/agent/v1/agentv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rebuiltNotice = "weave was rebuilt"

type fakeBinary struct {
	isReplaced bool
	asked      int
}

func (b *fakeBinary) IsReplaced() bool {
	b.asked++

	return b.isReplaced
}

// TestRebuilt covers the binary changing under a running panel.
func TestRebuilt(t *testing.T) {
	t.Parallel()

	t.Run("a replaced binary gives the window back and puts up the rebuilt screen", func(t *testing.T) {
		t.Parallel()

		binary := &fakeBinary{}
		harness := openRebuilt(t, binary)
		require.Equal(t, "%9", harness.panel.ReadGuestPane())
		require.NotContains(t, harness.readFrame(), rebuiltNotice)
		require.Equal(t, 1, binary.asked, "the file is looked at once per refresh")

		binary.isReplaced = true
		harness.fireTimers()

		assert.Contains(t, harness.readFrame(), rebuiltNotice)
		assert.Empty(t, harness.panel.ReadGuestPane(), "the agent's pane goes back to its own window")
		assert.True(t, doesQuit(harness.panel.Update(ui.NewKeyPress("x"))), "any key closes it")

		harness.fireTimers()
		assert.Equal(t, 2, binary.asked, "the refresh loop stops")
	})

	t.Run("a panel with no binary to watch stays up", func(t *testing.T) {
		t.Parallel()

		harness := openRebuilt(t, nil)
		harness.send(ui.NewTick(time.Now()))

		assert.NotContains(t, harness.readFrame(), rebuiltNotice)
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a panel that is busy is not taken over", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			busy func(harness *panelHarness)
		}{
			{
				name: "a write in flight",
				busy: func(harness *panelHarness) {
					harness.hold(agentv1connect.AgentServiceSetHiddenProcedure)
					harness.press("h")
				},
			},
			{
				name: "an open form",
				busy: func(harness *panelHarness) {
					harness.press("c")
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				binary := &fakeBinary{}
				harness := openRebuilt(t, binary)
				binary.isReplaced = true
				binary.asked = 0
				tt.busy(harness)

				harness.send(ui.NewTick(time.Now()))

				assert.NotContains(t, harness.readFrame(), rebuiltNotice)
				assert.Equal(t, "%9", harness.panel.ReadGuestPane(), "the window is kept")
				assert.Zero(t, binary.asked, "the file is not looked at while the panel is busy")
			})
		}
	})

	t.Run("a review message being written is not taken over", func(t *testing.T) {
		t.Parallel()

		binary := &fakeBinary{isReplaced: true}
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{binary: binary})
		harness.press("c")
		binary.asked = 0

		harness.send(ui.NewTick(time.Now()))

		assert.NotContains(t, harness.readFrame(), rebuiltNotice)
		assert.Zero(t, binary.asked)
	})
}

func openRebuilt(t *testing.T, binary ui.BinaryWatcher) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{binary: binary})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "api"), newRunning()))
	harness.press("j")
	harness.fireTimers()

	return harness
}

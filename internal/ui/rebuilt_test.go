package ui_test

import (
	"testing"
	"time"

	"komrad/internal/ui"
	"komrad/internal/ui/paneltest"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/task/v1/taskv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRebuilt covers the binary changing under a running panel.
func TestRebuilt(t *testing.T) {
	t.Parallel()

	t.Run("a replaced binary gives the window back and puts up the rebuilt screen", func(t *testing.T) {
		t.Parallel()

		binary := &fakeBinary{}
		harness := openRebuilt(t, binary)
		require.Equal(t, "%9", harness.Tmux().GuestPane())
		require.NotContains(t, harness.Frame(), rebuiltNotice)
		require.Equal(t, 1, binary.asked, "the file is looked at once per refresh")

		binary.isReplaced = true
		harness.Advance(time.Second)

		assert.Contains(t, harness.Frame(), rebuiltNotice)
		assert.Empty(t, harness.Tmux().GuestPane(), "the agent's pane goes back to its own window")
		assert.True(t, harness.Quits("x"), "any key closes it")

		harness.Advance(time.Second)
		assert.Equal(t, 2, binary.asked, "the refresh loop stops")
	})

	t.Run("a panel with no binary to watch stays up", func(t *testing.T) {
		t.Parallel()

		harness := openRebuilt(t, nil)
		harness.Advance(time.Second)

		assert.NotContains(t, harness.Frame(), rebuiltNotice)
		assert.Equal(t, "%9", harness.Tmux().GuestPane())
	})

	t.Run("a panel that is busy is not taken over", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			busy func(harness *paneltest.Panel)
		}{
			{
				name: "a write in flight",
				busy: func(harness *paneltest.Panel) {
					harness.Hold(taskv1connect.TaskServiceSetTaskHiddenProcedure)
					harness.Press("space", "h", "h")
				},
			},
			{
				name: "an open form",
				busy: func(harness *paneltest.Panel) {
					harness.Press("space", "c")
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

				harness.Advance(time.Second)

				assert.NotContains(t, harness.Frame(), rebuiltNotice)
				assert.Equal(t, "%9", harness.Tmux().GuestPane(), "the window is kept")
				assert.Zero(t, binary.asked, "the file is not looked at while the panel is busy")
			})
		}
	})

	t.Run("a confirmed archive still in flight keeps a rebuilt panel up", func(t *testing.T) {
		binary := &fakeBinary{}
		harness := paneltest.Start(t, paneltest.Params{Binary: binary})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(newFormWorkflow(), newFormAuthor()))
		harness.Hold(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.Press("g", "g", "j", "space", "a", "enter")

		binary.isReplaced = true
		harness.Advance(time.Second)
		require.NotContains(t, harness.Frame(), "komrad was rebuilt", "the write is still in flight")

		harness.Release(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.Watch(paneltest.Snapshot(newFormWorkflow()))
		harness.Advance(time.Second)
		assert.Contains(t, harness.Frame(), "komrad was rebuilt")
	})

	t.Run("a review that is busy is not taken over", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			press []string
		}{
			{
				name:  "a release being written",
				press: []string{"space", "S"},
			},
			{
				name:  "an editor open over an agent's work",
				press: []string{"space", "e"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				binary := &fakeBinary{isReplaced: true}
				harness, reviewed := startReviewed(t, paneltest.Params{Binary: binary})
				harness.Watch(reviewed.snapshot())
				harness.Press("space", "r")
				harness.Press(tt.press...)
				binary.asked = 0

				harness.Advance(time.Second)

				assert.NotContains(t, harness.Frame(), rebuiltNotice)
				assert.Zero(t, binary.asked)
			})
		}
	})
}

const rebuiltNotice = "komrad was rebuilt"

type fakeBinary struct {
	isReplaced bool
	asked      int
}

func (b *fakeBinary) IsReplaced() bool {
	b.asked++

	return b.isReplaced
}

func openRebuilt(t *testing.T, binary ui.BinaryWatcher) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{Binary: binary})
	harness.AnswerWorktreeReads()
	running := paneltest.Running()
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "api"), running, paneltest.TaskOf(running)))
	harness.Press("j")
	harness.Advance(time.Second)

	return harness
}

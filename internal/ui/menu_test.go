package ui_test

import (
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/panes"
	"komrad/internal/ui"
	"komrad/proto/gen/log/v1/logv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestMenu covers the menu space opens from any screen, the hovered row's entries over the panel's own.
func TestMenu(t *testing.T) {
	t.Parallel()

	t.Run("a screen with no hovered row opens only the panel's entries", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, newLiveWorkflow("w1", "api"))
		harness.press("space")

		frame := harness.readFrame()
		assert.Contains(t, frame, "  komrad")
		assert.Contains(t, frame, "› scratch")
		assert.NotContains(t, frame, "logs")
	})

	t.Run("an agent's entries sit over the panel's, and the cursor steps over the rule", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "quiet")
		require.Contains(t, harness.readFrame(), "› documents")
		require.Contains(t, harness.readFrame(), "───")

		harness.press("down", "down", "down", "down", "down")

		assert.Contains(t, harness.readFrame(), "› scratch")
	})

	t.Run("the legend offers it off an agent", func(t *testing.T) {
		t.Parallel()

		harness := openSchedules(t, buildSchedules()...)
		harness.press(widenListPresses...)
		harness.press("?")

		assert.Regexp(t, `space +menu`, readLegend(harness.readFrame()))
	})
}

// TestScratch covers the engine's scratch file opened in your editor from the menu.
func TestScratch(t *testing.T) {
	t.Parallel()

	t.Run("it opens the engine's file beside the list, creating it", func(t *testing.T) {
		t.Parallel()

		harness, dir := openScratchPanel(t, buildSchedules()...)
		harness.press("space", "s")

		path := filepath.Join(dir, "local.md")
		require.FileExists(t, path)
		require.NotEmpty(t, harness.panel.ReadRespawns())
		assert.Contains(t, lastOf(harness.panel.ReadRespawns()), "'"+path+"'")
		assert.Equal(t, dir, lastOf(harness.panel.ReadRespawnDirs()))
	})

	t.Run("an open file keeps what was written in it", func(t *testing.T) {
		t.Parallel()

		harness, dir := openScratchPanel(t, buildSchedules()...)
		path := filepath.Join(dir, "local.md")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(path, []byte("notes"), 0o600))

		harness.press("space", "s")

		kept, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "notes", string(kept))
	})

	t.Run("closing it offers it again", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			close func(*panelHarness)
		}{
			{
				name:  "the entry again",
				close: func(h *panelHarness) { h.press("space", "s") },
			},
			{
				name: "the editor quitting",
				close: func(h *panelHarness) {
					h.panel.SetPaneDead(lastOf(h.panel.ReadRespawnPanes()))
					h.fireTimers()
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness, _ := openScratchPanel(t, newLiveWorkflow("w1", "api"), newRunning())
				harness.press("j")
				harness.fireTimers()
				harness.press("space", "s", "space")
				require.Empty(t, harness.panel.ReadGuestPane())
				require.Contains(t, harness.readFrame(), "close scratch")
				harness.press("esc")

				tt.close(harness)
				harness.fireTimers()
				harness.press("space")

				assert.NotContains(t, harness.readFrame(), "close scratch")
				assert.Equal(t, "%9", harness.panel.ReadGuestPane(), "the hovered agent goes back on the stage")
			})
		}
	})

	t.Run("moving the cursor keeps it on the stage", func(t *testing.T) {
		t.Parallel()

		harness, _ := openScratchPanel(t, buildSubpagesAgents()...)
		harness.press("space", "s")
		respawns, swaps := harness.panel.CountRespawns(), harness.panel.CountSwaps()

		harness.press("j")
		harness.fireTimers()

		assert.Equal(t, respawns, harness.panel.CountRespawns())
		assert.Equal(t, swaps, harness.panel.CountSwaps())
	})

	t.Run("the window comes back from the menu with the editor already on the stage", func(t *testing.T) {
		t.Parallel()

		harness, _ := openScratchPanel(t, newLiveWorkflow("w1", "api"), newRunning())
		harness.press("j")
		harness.fireTimers()
		harness.press("space")
		zooms := harness.panel.ReadZooms()
		require.NotEmpty(t, zooms)
		require.True(t, zooms[len(zooms)-1], "the menu holds the window")
		sent := len(harness.panel.ReadMoveBatches())

		harness.press("s")

		batches := harness.panel.ReadMoveBatches()[sent:]
		require.NotEmpty(t, batches)
		assert.Contains(t, batches[0], panes.KindZoom)
		assert.Contains(t, batches[0], panes.KindRespawn)
	})

	t.Run("the logs under it keep reading once it closes", func(t *testing.T) {
		t.Parallel()

		harness, _ := openScratchPanel(t, buildSubpagesAgents()...)
		tailOfTwo().answer(harness)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "l")

		harness.press("space", "s", "space", "s")
		harness.fireTimers()

		harness.assertSentTo(logv1connect.LogServiceReadAgentLogsProcedure, logRead("service", ""), logRead("service", "10"))
	})

	t.Run("a created agent taking the stage closes it", func(t *testing.T) {
		t.Parallel()

		harness, _ := openScratchPanel(t, newLiveWorkflow("w1", "api"), newAgent("a1", "old", "w1"))
		answerAgentCreate(harness, "%9")
		harness.press("j", "space", "s")

		harness.press("c", "h", "i", "ctrl+s")
		harness.press("space")

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.NotContains(t, harness.readFrame(), "close scratch")
	})

	t.Run("it and another editor never share the stage", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			first []string
			then  []string
			want  string
		}{
			{
				name:  "the worktree editor over it",
				first: []string{"space", "s"},
				then:  []string{"e"},
				want:  "could not open the editor",
			},
			{
				name:  "it over the worktree editor",
				first: []string{"e"},
				then:  []string{"space", "s"},
				want:  "could not open the scratch file",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness, _ := openScratchPanel(t, buildSubpagesAgents()...)
				moveToAgentRow(t, harness, "author")
				harness.press(tt.first...)
				respawns := harness.panel.CountRespawns()

				harness.press(tt.then...)

				assert.Contains(t, harness.readFrame(), tt.want)
				assert.Equal(t, respawns, harness.panel.CountRespawns())
			})
		}
	})
}

func openScratchPanel(t *testing.T, rows ...proto.Message) (*panelHarness, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "scratch")
	harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{Editor: "vi", ScratchDir: dir}})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(rows...))

	return harness, dir
}

func lastOf(values []string) string {
	if len(values) == 0 {
		return ""
	}

	return values[len(values)-1]
}

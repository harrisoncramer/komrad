package ui_test

import (
	"errors"
	"strings"
	"testing"

	"komrad/internal/panes"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVisualMode covers leader v in the sidebar handing tmux's copy-mode a stand-in drawn as the
// whole sidebar, copying only from the block that has the keys.
func TestVisualMode(t *testing.T) {
	tests := []struct {
		name       string
		open       func(t *testing.T) *panelHarness
		from       []string
		want       string
		wantCursor string
		wantAbsent []string
	}{
		{
			name:       "the list is copied from with the cursor on its row",
			open:       openFocus,
			wantCursor: "a1",
		},
		{
			name:       "the record is copied from without the list above it",
			open:       openFocus,
			from:       []string{"pick:record"},
			wantCursor: "name",
			wantAbsent: []string{"komrad › agents"},
		},
		{
			name:       "the column is copied from without the list beside it",
			open:       openFailedSetup,
			from:       []string{"ctrl+space", "ctrl+space"},
			want:       "the worktree could not be made",
			wantAbsent: []string{"komrad › agents", "│"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := tt.open(t)
			harness.press(tt.from...)

			harness.press("ctrl+space", "v")

			standIns := harness.panel.ReadStandIns()
			require.Len(t, standIns, 1)
			assert.Equal(t, "%0", standIns[0].Pane)
			assert.Equal(t, []string{"C-Space", "v"}, standIns[0].Keys, "the stand-in says when copy-mode ends")

			text := ansi.Strip(strings.Join(standIns[0].Rows, "\n"))
			assert.Contains(t, text, "komrad › agents", "the rest of the sidebar stays drawn")

			block := cutBlock(text, standIns[0].Focus)
			assert.Contains(t, block, tt.want)
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, block, absent)
			}

			if tt.wantCursor == "" {
				return
			}

			rows := strings.Split(text, "\n")
			require.Greater(t, len(rows), standIns[0].Row)
			from := ansi.Cut(rows[standIns[0].Row], standIns[0].Col, ansi.StringWidth(rows[standIns[0].Row]))
			assert.True(t, strings.HasPrefix(from, "› "), "the cursor lands on the row cursor, got %q", from)
			assert.Contains(t, from, tt.wantCursor)
		})
	}

	t.Run("copy-mode ending lets the window move again", func(t *testing.T) {
		harness := openFocus(t)

		harness.press("ctrl+space", "v")
		harness.press("ctrl+space", "j")
		assert.NotContains(t, harness.readDrawn(), "JUMP", "the picker waits while the stand-in has the sidebar's place")

		harness.press("ctrl+space", "v")
		harness.press("ctrl+space", "j")
		assert.Contains(t, harness.readDrawn(), "JUMP")
		assert.Len(t, harness.panel.ReadStandIns(), 1)
	})

	t.Run("a stand-in tmux refuses says so and leaves v to try again", func(t *testing.T) {
		harness := openFocus(t)
		harness.panel.RefuseStandIns(errors.New("no space for a window"))

		harness.press("ctrl+space", "v")
		harness.fireTimers()

		opened := readOpenMessages(harness)
		assert.Contains(t, opened, "Could not start visual mode")
		assert.Contains(t, opened, "no space for a window")

		harness.panel.RefuseStandIns(nil)
		harness.press("ctrl+space", "v")
		assert.Len(t, harness.panel.ReadStandIns(), 2)
	})
}

func openFailedSetup(t *testing.T) *panelHarness {
	t.Helper()

	failed := newDormant("login")
	failed.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	failed.FailureReason = "the worktree could not be made"

	harness := newPanelHarness(t, panelHarnessParams{})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), failed))
	harness.press("j")
	harness.fireTimers()

	return harness
}

func cutBlock(text string, block panes.Rect) string {
	rows := strings.Split(text, "\n")

	var cut []string
	for y := block.Top; y < min(block.Top+block.Height, len(rows)); y++ {
		cut = append(cut, ansi.Cut(rows[y], block.Left, block.Left+block.Width))
	}

	return strings.Join(cut, "\n")
}

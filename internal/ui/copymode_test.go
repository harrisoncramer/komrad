package ui_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"komrad/internal/panes"
	"komrad/internal/ui/paneltest"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVisualMode covers leader v in the sidebar handing tmux's copy-mode a stand-in rendered as the
// whole sidebar, copying only from the block that has the keys.
func TestVisualMode(t *testing.T) {
	tests := []struct {
		name       string
		open       func(t *testing.T) *paneltest.Panel
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
			wantCursor: "agent",
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
			harness.Press(tt.from...)

			harness.Press("ctrl+space", "v")

			standIns := harness.Tmux().StandIns()
			require.Len(t, standIns, 1)
			assert.Equal(t, "%0", standIns[0].Pane)
			assert.Equal(t, []string{"C-Space", "v"}, standIns[0].Keys, "the stand-in says when copy-mode ends")

			text := ansi.Strip(strings.Join(standIns[0].Rows, "\n"))
			assert.Contains(t, text, "komrad › agents", "the rest of the sidebar stays rendered")

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

		harness.Press("ctrl+space", "v")
		harness.Press("ctrl+space", "j")
		assert.NotContains(t, harness.Rendered(), "JUMP", "the picker waits while the stand-in has the sidebar's place")

		harness.Press("ctrl+space", "v")
		harness.Press("ctrl+space", "j")
		assert.Contains(t, harness.Rendered(), "JUMP")
		assert.Len(t, harness.Tmux().StandIns(), 1)
	})

	t.Run("a stand-in tmux refuses says so and leaves v to try again", func(t *testing.T) {
		harness := openFocus(t)
		harness.Tmux().RefuseStandIns(errors.New("no space for a window"))

		harness.Press("ctrl+space", "v")
		harness.Advance(time.Second)

		opened := harness.ReadMessages()
		assert.Contains(t, opened, "Could not start visual mode")
		assert.Contains(t, opened, "no space for a window")

		harness.Tmux().RefuseStandIns(nil)
		harness.Press("ctrl+space", "v")
		assert.Len(t, harness.Tmux().StandIns(), 2)
	})
}

func openFailedSetup(t *testing.T) *paneltest.Panel {
	t.Helper()

	failed := newDormant("login")
	failed.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	failed.FailureReason = "the worktree could not be made"

	harness := paneltest.Start(t, paneltest.Params{})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), failed))
	harness.Press("j")
	harness.Advance(time.Second)

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

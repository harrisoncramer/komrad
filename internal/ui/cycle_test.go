package ui_test

import (
	"strings"
	"testing"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewCycle covers the two keys that move the keyboard between the list, the agent and the
// record.
func TestViewCycle(t *testing.T) {
	sidebar, agent := "%0", "%9"

	tests := []struct {
		name         string
		press        []string
		from         string
		wantSelected []string
		wantRecord   bool
	}{
		{
			name:         "forward from the list goes to the agent",
			press:        []string{"ctrl+a"},
			wantSelected: []string{agent},
		},
		{
			name:       "backward from the list goes to the record",
			press:      []string{"ctrl+q"},
			wantRecord: true,
		},
		{
			name:         "backward from the record goes to the agent",
			press:        []string{"ctrl+q", "ctrl+q"},
			wantSelected: []string{agent},
			wantRecord:   true,
		},
		{
			name:  "forward from the record wraps to the list",
			press: []string{"ctrl+q", "ctrl+a"},
		},
		{
			name:         "forward from the agent's pane goes to its record",
			press:        []string{"ctrl+a"},
			from:         agent,
			wantSelected: []string{sidebar},
			wantRecord:   true,
		},
		{
			name:         "backward from the agent's pane goes to the list",
			press:        []string{"ctrl+q"},
			from:         agent,
			wantSelected: []string{sidebar},
		},
		{
			name:         "forward from the info pane goes to the list",
			press:        []string{"ctrl+a"},
			from:         "%77",
			wantSelected: []string{sidebar},
		},
		{
			name:         "backward from the info pane goes to the record",
			press:        []string{"ctrl+q"},
			from:         "%77",
			wantSelected: []string{sidebar},
			wantRecord:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFocus(t)
			require.Equal(t, agent, harness.panel.ReadGuestPane())
			if tt.from != "" {
				harness.panel.SetActivePane(tt.from)
			}
			selected := len(harness.panel.ReadSelectedPanes())

			harness.press(tt.press...)

			assert.ElementsMatch(t, tt.wantSelected, harness.panel.ReadSelectedPanes()[selected:])
			assert.Equal(t, tt.wantRecord, readRecordCursor(harness.readFrame()) != "")
		})
	}

	t.Run("a press from the agent's pane lands in a search being typed without moving past it", func(t *testing.T) {
		harness := openFocus(t)
		harness.press("/", "a")
		_, bound := harness.panel.ReadRootBinding("C-a")
		require.True(t, bound, "typing a search leaves the agent on the stage")
		harness.panel.SetActivePane(agent)
		selected := len(harness.panel.ReadSelectedPanes())

		harness.press("ctrl+a")

		frame := harness.readFrame()
		assert.Equal(t, []string{sidebar}, harness.panel.ReadSelectedPanes()[selected:])
		assert.Contains(t, frame, "/ a▏", "the search is still being typed")
		assert.Empty(t, readRecordCursor(frame))
	})

	t.Run("the sort strip hands the keys back to the list and the cycle moves on from there", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.press("f", "ctrl+a")

		assert.NotEmpty(t, readRecordCursor(harness.readFrame()))
	})

	t.Run("a configured pair moves the keys and leaves the default pair to the panes", func(t *testing.T) {
		keys := config.DefaultKeybindings()
		keys.Cycle.Views = []string{"alt+n", "alt+p"}
		harness := newPanelHarness(t, panelHarnessParams{keys: &keys})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildFocusRows()...))

		for _, key := range []string{"M-n", "M-p"} {
			command, bound := harness.panel.ReadRootBinding(key)
			require.True(t, bound, key)
			assert.Equal(t, []string{"send-keys", "-t", sidebar, key}, command)
		}
		_, bound := harness.panel.ReadRootBinding("C-a")
		assert.False(t, bound)

		selected := len(harness.panel.ReadSelectedPanes())

		harness.press("alt+n")

		assert.Equal(t, []string{agent}, harness.panel.ReadSelectedPanes()[selected:])
	})

	t.Run("the legend names the pair as one row", func(t *testing.T) {
		harness := openHelp(t, panelHarnessParams{height: 50}, "}", "}", "}", "}", "?")

		assert.Regexp(t, `C-a/C-q +next/prev view`, readLegend(harness.readFrame()))
	})
}

// TestReviewViewCycle covers the cycle on the review, where the thread under the cursor is a stop.
func TestReviewViewCycle(t *testing.T) {
	tests := []struct {
		name       string
		press      []string
		wantRecord bool
		wantThread bool
	}{
		{
			name:       "forward from the diff goes to the thread",
			press:      []string{"ctrl+a"},
			wantThread: true,
		},
		{
			name:       "forward from the thread goes to the record",
			press:      []string{"ctrl+a", "ctrl+a"},
			wantRecord: true,
		},
		{
			name:  "forward from the record wraps to the diff with the thread let go",
			press: []string{"ctrl+a", "ctrl+a", "ctrl+a"},
		},
		{
			name:  "leaving the record by its own key lands on the diff rather than the thread",
			press: []string{"ctrl+a", "ctrl+a", "esc"},
		},
		{
			name:       "backward from the record goes to the thread",
			press:      []string{"ctrl+q", "ctrl+q"},
			wantThread: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openLongThread(t)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, tt.wantRecord, readRecordCursor(frame) != "")
			assert.Equal(t, tt.wantThread, strings.Contains(readLegend(frame), "back to the diff"))
		})
	}
}

// TestTerminalViewCycle covers the cycle with a shell open below the stage, where the shell is a
// stop between the agent and the record.
func TestTerminalViewCycle(t *testing.T) {
	sidebar, agent := "%0", "%9"

	tests := []struct {
		name         string
		press        []string
		from         string
		wantSelected []string
		wantRecord   bool
	}{
		{
			name:         "forward from the agent's pane goes to the shell",
			press:        []string{"ctrl+a"},
			from:         agent,
			wantSelected: []string{infoPane},
		},
		{
			name:         "forward from the shell goes to the record",
			press:        []string{"ctrl+a"},
			from:         infoPane,
			wantSelected: []string{sidebar},
			wantRecord:   true,
		},
		{
			name:         "backward from the shell goes to the agent's pane",
			press:        []string{"ctrl+q"},
			from:         infoPane,
			wantSelected: []string{agent},
		},
		{
			name:         "backward from the record goes to the shell",
			press:        []string{"ctrl+q", "ctrl+q"},
			wantSelected: []string{infoPane},
			wantRecord:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))
			harness.press("space", "t")
			if tt.from != "" {
				harness.panel.SetActivePane(tt.from)
			}
			selected := len(harness.panel.ReadSelectedPanes())

			harness.press(tt.press...)

			assert.ElementsMatch(t, tt.wantSelected, harness.panel.ReadSelectedPanes()[selected:])
			assert.Equal(t, tt.wantRecord, readRecordCursor(harness.readFrame()) != "")
		})
	}

	t.Run("on the schedules the shell is the stop past the list", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))
		harness.press("space", "t", "tab", "s")
		selected := len(harness.panel.ReadSelectedPanes())

		harness.press("ctrl+a")

		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes()[selected:])
	})

	t.Run("a shell with no agent staged holds the keys", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", t.TempDir()))
		_, bound := harness.panel.ReadRootBinding("C-a")
		require.False(t, bound, "nothing is beside the sidebar yet")

		harness.press("space", "t")

		command, bound := harness.panel.ReadRootBinding("C-a")
		require.True(t, bound)
		assert.Equal(t, []string{"send-keys", "-t", sidebar, "C-a"}, command)
	})
}

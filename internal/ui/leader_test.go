package ui_test

import (
	"testing"

	"komrad/internal/panes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeader covers the leader and the keys that follow it.
func TestLeader(t *testing.T) {
	sidebar, agent := "%0", "%9"

	t.Run("the leader opens its table from every pane of the panel's window alone", func(t *testing.T) {
		harness := openFocus(t)

		command, bound := harness.panel.ReadRootBinding("C-Space")
		require.True(t, bound)
		assert.Equal(t, []string{"if-shell", "-F", "#{==:#{window_id},@1}", "switch-client -T komrad-leader", "send-keys C-Space"}, command)

		for _, key := range []string{"v", "p"} {
			command, bound := harness.panel.ReadTableBinding("komrad-leader", key)
			require.True(t, bound, key)
			assert.Equal(t, []string{"send-keys", "-t", sidebar, "C-Space", key}, command)
		}

		for _, key := range []string{"C-Space", "Space"} {
			command, bound := harness.panel.ReadTableBinding("komrad-leader", key)
			require.True(t, bound, key)
			assert.Equal(t, []string{"if-shell", "-F", "#{window_zoomed_flag}", "", "last-pane"}, command)
		}
	})

	t.Run("v freezes the window and v again thaws it", func(t *testing.T) {
		harness := openFocus(t)
		harness.panel.SetScreens(
			[]panes.Rect{{Pane: sidebar, Width: 40, Height: 3}, {Pane: agent, Left: 41, Width: 30, Height: 3}},
			map[string]string{sidebar: "the sidebar as drawn\n", agent: "the agent as drawn\n"},
		)

		harness.press("ctrl+space", "v")

		frame := harness.readDrawn()
		assert.Contains(t, frame, "the agent as drawn")
		assert.Contains(t, frame, "VISUAL")
		assert.True(t, harness.panel.IsZoomed())

		harness.press("ctrl+space", "v")

		assert.NotContains(t, harness.readDrawn(), "VISUAL")
		assert.False(t, harness.panel.IsZoomed())
	})

	t.Run("an unknown key after the leader is dropped", func(t *testing.T) {
		harness := openFocus(t)
		selected := len(harness.panel.ReadSelectedPanes())

		harness.press("ctrl+space", "ctrl+a")

		assert.Empty(t, harness.panel.ReadSelectedPanes()[selected:], "the cycle key went to the leader")
	})
}

// TestPanePicker covers the row of panes the leader opens.
func TestPanePicker(t *testing.T) {
	sidebar, agent := "%0", "%9"

	tests := []struct {
		name         string
		from         string
		press        []string
		wantSelected []string
		wantRecord   bool
	}{
		{
			name:         "a pane's letter lands the keys on it",
			press:        []string{"a"},
			wantSelected: []string{agent},
		},
		{
			name:       "the details letter lands on the record",
			press:      []string{"d"},
			wantRecord: true,
		},
		{
			name:         "opened from the agent's pane it takes the keys to the sidebar to pick",
			from:         agent,
			press:        []string{"l"},
			wantSelected: []string{sidebar},
		},
		{
			name:         "esc gives the keys back to the pane that had them",
			from:         agent,
			press:        []string{"esc"},
			wantSelected: []string{sidebar, agent},
		},
		{
			name:         "tab moves along the row and enter lands",
			press:        []string{"tab", "enter"},
			wantSelected: []string{agent},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFocus(t)
			if tt.from != "" {
				harness.panel.SetActivePane(tt.from)
			}
			selected := len(harness.panel.ReadSelectedPanes())

			harness.press("ctrl+space", "p")
			harness.press(tt.press...)

			assert.Equal(t, tt.wantSelected, harness.panel.ReadSelectedPanes()[selected:])
			assert.Equal(t, tt.wantRecord, readRecordCursor(harness.readFrame()) != "")
		})
	}

	t.Run("the row names each pane and the legend its letters", func(t *testing.T) {
		harness := openFocus(t)

		harness.press("ctrl+space", "p")

		frame := harness.readFrame()
		assert.Regexp(t, `list +agent +details`, frame)
		assert.Regexp(t, `a +agent`, readLegend(frame))
	})
}

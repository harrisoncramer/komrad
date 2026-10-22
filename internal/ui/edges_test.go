package ui_test

import (
	"testing"

	"komrad/internal/ui/paneltest"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEdges covers the leader's keys that move an edge of the region holding the keys.
func TestEdges(t *testing.T) {
	t.Run("the edge keys repeat from every pane of the panel's window", func(t *testing.T) {
		harness := openFocus(t)

		for _, key := range []string{"h", "l"} {
			command, bound := harness.Tmux().TableBinding("komrad-leader", key)
			require.True(t, bound, key)
			assert.Equal(t, []string{"send-keys", "-t", "%0", "C-Space", key}, command)
			assert.True(t, harness.Tmux().IsRepeatBinding("komrad-leader", key), key)
		}
	})

	tests := []struct {
		name string
		// keys moves the keyboard to the region under test.
		keys          func(*paneltest.Panel, *reviewServer)
		press         string
		wantListDelta int
		wantPaneDelta int
	}{
		{
			name:          "on the diff, h widens it into the list",
			press:         "h",
			wantListDelta: -4,
		},
		{
			name:          "on the diff, l widens it into the writer",
			press:         "l",
			wantPaneDelta: 4,
		},
		{
			name:          "on the files, h narrows the list",
			keys:          func(harness *paneltest.Panel, _ *reviewServer) { harness.Press("ctrl+space", "ctrl+space") },
			press:         "h",
			wantListDelta: -4,
		},
		{
			name:          "on the files, l widens the list",
			keys:          func(harness *paneltest.Panel, _ *reviewServer) { harness.Press("ctrl+space", "ctrl+space") },
			press:         "l",
			wantListDelta: 4,
		},
		{
			name:          "on the writer, h widens it",
			keys:          focusWriter,
			press:         "h",
			wantPaneDelta: -4,
		},
		{
			name:          "on the writer, l narrows it",
			keys:          focusWriter,
			press:         "l",
			wantPaneDelta: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newReviewServer(t)
			harness := openReview(t, server)
			require.True(t, isBeside(harness))
			if tt.keys != nil {
				tt.keys(harness, server)
			}
			list := paneltest.ListWidth(harness.Frame())
			pane := harness.Tmux().SidebarWidth()

			harness.Press("ctrl+space", tt.press)

			assert.Equal(t, tt.wantListDelta, paneltest.ListWidth(harness.Frame())-list, "the list")
			assert.Equal(t, tt.wantPaneDelta, harness.Tmux().SidebarWidth()-pane, "the edge against the writer")
		})
	}

	t.Run("beside an agent on the stage, both keys move the one edge from either side", func(t *testing.T) {
		for _, elsewhere := range []bool{false, true} {
			harness := openFocus(t)
			if elsewhere {
				harness.Tmux().SetActivePane(harness.Tmux().StagePane())
				harness.Send(tea.BlurMsg{})
			}
			pane := harness.Tmux().SidebarWidth()

			harness.Press("ctrl+space", "l")
			assert.Equal(t, pane+4, harness.Tmux().SidebarWidth(), "elsewhere %v", elsewhere)

			harness.Press("ctrl+space", "h", "ctrl+space", "h")
			assert.Equal(t, pane-4, harness.Tmux().SidebarWidth(), "elsewhere %v", elsewhere)
		}
	})
}

func focusWriter(harness *paneltest.Panel, server *reviewServer) {
	harness.Tmux().SetActivePane(server.agent.GetPaneId())
	harness.Send(tea.BlurMsg{})
}

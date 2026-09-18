package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/view"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// symbologyModel is a panel holding one workflow and one agent under it, drawn either way
// according to the setting handed in.
func symbologyModel(symbology bool) model {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		sidebar: config.Sidebar{Symbology: symbology},
		width:   120,
		height:  40,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{{
				agent: agentstore.Agent{
					ID:         "a1",
					Name:       "worker",
					WorkflowID: "w1",
					State:      agentstore.StateFailedToCreate,
				},
			}},
		},
	}
	m.tree.cursor = -1

	return m
}

// TestSymbologyIsOffByDefault pins that the list is drawn the way it always has been until the
// setting says otherwise.
func TestSymbologyIsOffByDefault(t *testing.T) {
	assert.False(t, config.Defaults().Sidebar.Symbology)
}

// TestSymbologySwitchPicksThePath pins which of the two drawings makes a row, the status bar
// standing where the old drawing put a state icon.
func TestSymbologySwitchPicksThePath(t *testing.T) {
	tests := []struct {
		name      string
		symbology bool
		want      string
		absent    string
	}{
		{
			name:   "the old drawing gives a failed create its state icon",
			want:   "✗",
			absent: "▍",
		},
		{
			name:      "the newer drawing gives it a broken bar instead",
			symbology: true,
			want:      "▍",
			absent:    "✗",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, ok := rowNamed(strings.Split(symbologyModel(tt.symbology).agentsView(), "\n"), "worker")
			require.True(t, ok)

			assert.Contains(t, ansi.Strip(row), tt.want)
			assert.NotContains(t, ansi.Strip(row), tt.absent)
		})
	}
}

// TestSymbologyDrawsTheColumnHeader pins the row of column names the newer drawing stands its
// list under.
func TestSymbologyDrawsTheColumnHeader(t *testing.T) {
	header := ansi.Strip(view.Header(paint.Colored, view.Columns{Repo: true, Review: true, Forge: true}))

	assert.NotContains(t, ansi.Strip(symbologyModel(false).agentsView()), header)
	assert.Contains(t, ansi.Strip(symbologyModel(true).agentsView()), header)
}

// TestSymbologyOnAnEmptyPanel pins the first run, where a panel holding nothing at all still
// draws its chrome and says where to start.
func TestSymbologyOnAnEmptyPanel(t *testing.T) {
	for _, symbology := range []bool{false, true} {
		m := model{
			frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
			keys:    config.DefaultKeybindings(),
			sidebar: config.Sidebar{Symbology: symbology},
			width:   120,
			height:  40,
		}
		m.tree.cursor = -1

		assert.NotPanics(t, func() { m.agentsView() })
		assert.Contains(t, ansi.Strip(m.agentsView()), "Agents and Workflows")
		assert.Contains(t, m.agentDoc(), "nothing here yet")
	}
}

// longListModel is a panel holding one workflow with more agents under it than its pane has
// room for, the cursor standing on the row named.
func longListModel(cursor int) model {
	agents := make([]agentItem, 0, 40)
	for i := range 40 {
		agents = append(agents, agentItem{agent: agentstore.Agent{
			ID:         fmt.Sprintf("a%02d", i),
			Name:       fmt.Sprintf("agent-%02d", i),
			WorkflowID: "w1",
		}})
	}

	m := symbologyModel(true)
	m.height = 20
	m.listing.agents = agents
	m.tree.cursor = cursor

	return m
}

// TestSymbologyWindowsALongList pins that the newer drawing scrolls rather than running off the
// bottom of the pane, and that it says how much it is not showing.
func TestSymbologyWindowsALongList(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		cursor  int
		markers []string
	}{
		{
			name:    "at the top only what is below is counted",
			want:    "weave",
			cursor:  0,
			markers: []string{"↓"},
		},
		{
			name:    "in the middle both ends are",
			want:    "agent-19",
			cursor:  20,
			markers: []string{"↑", "↓"},
		},
		{
			name:    "at the bottom only what is above is",
			want:    "agent-39",
			cursor:  40,
			markers: []string{"↑"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := longListModel(tt.cursor)
			drawn := ansi.Strip(m.agentsListView())

			require.Equal(t, m.height, paint.CountLines(drawn))
			assert.Contains(t, drawn, tt.want, "the row the cursor stands on is inside the window")

			for _, marker := range []string{"↑", "↓"} {
				assert.Equal(t, slices.Contains(tt.markers, marker), strings.Contains(drawn, marker),
					"the %q marker", marker)
			}
		})
	}
}

// TestSymbologyNarrowSidebarWindowsOneMoreRow pins the line a narrow sidebar saves by drawing no
// column header going to the list rather than being reserved for a header that is not there.
func TestSymbologyNarrowSidebarWindowsOneMoreRow(t *testing.T) {
	narrow, wide := longListModel(20), longListModel(20)
	narrow.width = 16

	assert.Equal(t, visibleAgentRows(t, wide)+1, visibleAgentRows(t, narrow))
}

// visibleAgentRows is how many of the agent rows a drawing put on screen.
func visibleAgentRows(t *testing.T, m model) int {
	t.Helper()

	count := 0
	for _, row := range strings.Split(ansi.Strip(m.agentsListView()), "\n") {
		if strings.Contains(row, "agent-") {
			count++
		}
	}

	return count
}

// TestSymbologyWindowFillsThePane pins the list being handed every line the pane has and no
// more, which the difference between two widths cannot see: budget one line too few and the
// whole drawing comes up a line short of the pane with nothing else to say so.
func TestSymbologyWindowFillsThePane(t *testing.T) {
	for _, width := range []int{16, 120} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := longListModel(20)
			m.width = width

			assert.Equal(t, m.height, paint.CountLines(ansi.Strip(m.agentsListView())))
		})
	}
}

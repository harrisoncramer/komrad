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

// agentsListModel is a panel holding one workflow and one agent under it.
func agentsListModel() model {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  120,
		height: 40,
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

// TestAgentsListDrawsTheColumnHeader pins the row of column names the list stands under.
func TestAgentsListDrawsTheColumnHeader(t *testing.T) {
	header := ansi.Strip(view.Header(paint.Colored, view.Columns{Repo: true, Review: true, Forge: true}))

	assert.Contains(t, ansi.Strip(agentsListModel().agentsView()), header)
}

// TestAgentsListOnAnEmptyPanel pins the first run, where a panel holding nothing at all still
// draws its chrome and says where to start.
func TestAgentsListOnAnEmptyPanel(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  120,
		height: 40,
	}
	m.tree.cursor = -1

	assert.NotPanics(t, func() { m.agentsView() })
	assert.Contains(t, ansi.Strip(m.agentsView()), "Agents and Workflows")
	assert.Contains(t, m.agentDoc(), "nothing here yet")
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

	m := agentsListModel()
	m.height = 20
	m.listing.agents = agents
	m.tree.cursor = cursor

	return m
}

// TestAgentsListWindowsALongList pins that the list scrolls rather than running off the bottom
// of the pane, and that it says how much it is not showing.
func TestAgentsListWindowsALongList(t *testing.T) {
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

// TestAgentsListWindowFillsThePane pins the list being handed every line the pane has and no
// more.
func TestAgentsListWindowFillsThePane(t *testing.T) {
	for _, width := range []int{16, 120} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := longListModel(20)
			m.width = width

			assert.Equal(t, m.height, paint.CountLines(ansi.Strip(m.agentsListView())))
		})
	}
}

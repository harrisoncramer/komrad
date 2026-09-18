package ui

import (
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

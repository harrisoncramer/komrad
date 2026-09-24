package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xptr"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizeModel gives a model a window wide enough for both halves and attaches its frame.
func sizeModel(m model, tmux *recordingTmux) model {
	tmux.width, tmux.height, tmux.windowWidth = 200, 40, 200
	m.width, m.height = 200, 40
	m.frame.Attach("%0")

	return m
}

// newPanelessModel is the panel on a row with no pane of its own.
func newPanelessModel(t *testing.T) (model, *recordingTmux) {
	t.Helper()

	tmux := &recordingTmux{}
	m := sizeModel(infoModel(tmux), tmux)
	m.listing.workflows = []panel.Workflow{{
		ID:      "w1",
		Name:    "backend",
		Context: "notes",
	}}
	m.agents.cursor = 0
	m.stageSelection()

	require.True(t, m.frame.IsSolo(), "the folder header has no pane, so the panel holds the window")

	return m, tmux
}

// TestAConfiguredWidthIsWidenedAndCapped covers the configured sidebar width being widened and capped.
func TestAConfiguredWidthIsWidenedAndCapped(t *testing.T) {
	tests := []struct {
		name    string
		columns int
		want    int
	}{
		{
			name:    "narrower than a list of names",
			columns: 8,
			want:    nameWidth,
		},
		{
			name:    "wider than the block's floor leaves",
			columns: 190,
			want:    200 - reviewDiffFloor,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{}
			m := sizeModel(infoModel(tmux), tmux)
			m.frame = layout.New(tmux, config.Layout{SidebarWidth: tt.columns})
			m.frame.Attach("%0")
			m.enterScreen(screenReview)
			m.review = reviewScreen{
				agentID:   "a1",
				agentName: "auth",
				loading:   true,
			}

			split, ok := m.screenColumnSplit()
			require.True(t, ok)
			assert.Equal(t, tt.want, split.sidebar)
		})
	}
}

// TestADraggedDividerIsDrawnWhereItIs covers the panel drawing its boundary at the dragged divider.
func TestADraggedDividerIsDrawnWhereItIs(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizeModel(infoModel(tmux), tmux)
	m.enterScreen(screenReview)
	m.review = reviewScreen{
		agentID:   "a1",
		agentName: "auth",
		loading:   true,
	}

	tmux.width = 25
	m.frame.SyncSplit()

	split, ok := m.screenColumnSplit()
	require.True(t, ok)
	assert.Equal(t, 174, split.sidebar, "the window less the stage and the divider")
	assert.Equal(t, 26, split.column, "and the block keeps what is left rather than its floor")
}

// TestASplitAgentsListIsLaidOutAgainstItsOwnColumn covers the list beside a document fitting its own column.
func TestASplitAgentsListIsLaidOutAgainstItsOwnColumn(t *testing.T) {
	withColor(t)

	m, _ := newPanelessModel(t)
	m.listing.agents = []panel.AgentRow{{Agent: panel.Agent{
		ID:         "a1",
		Name:       strings.Repeat("long-agent-name-", 6),
		WorkflowID: "w1",
		State:      panel.StateIdle,
		ScheduleID: xptr.Ptr("s1"),
	}}}
	m.agents.selected = selectAgents([]string{"a1"}...)

	split, ok := m.screenColumnSplit()
	require.True(t, ok, "the hovered header has no pane, so the panel draws a document beside its list")

	sidebar := m.measuredAt(split.sidebar)
	require.Equal(t, split.sidebar, sidebar.paneWidth())

	for i, row := range strings.Split(strings.TrimRight(sidebar.agentsListView(), "\n"), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(row), split.sidebar,
			"row %d reaches past its own column: %q", i, ansi.Strip(row))
	}
}

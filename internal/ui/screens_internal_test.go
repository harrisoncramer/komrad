package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xptr"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizedTo gives a model a window big enough to hold both halves and attaches its frame, which is
// what every screen drawing a block of its own needs before it will draw one.
func sizedTo(m model, tmux *recordingTmux) model {
	tmux.width, tmux.height, tmux.windowWidth = 200, 40, 200
	m.width, m.height = 200, 40
	m.frame.Attach("%0")

	return m
}

// resizedTo puts the terminal and the panel's pane at the same size.
func resizedTo(t *testing.T, m model, width, height int) model {
	t.Helper()

	tmux, ok := m.tmux.(*recordingTmux)
	require.True(t, ok)
	tmux.width, tmux.height = width, height

	sized, _ := m.route(tea.WindowSizeMsg{Width: width, Height: height})

	return asModel(t, sized)
}

// paneless is the panel on a row with no pane of its own.
func paneless(t *testing.T) (model, *recordingTmux) {
	t.Helper()

	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
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

// TestKeysMovingToAPaneTakeThemOffTheList covers the keyboard going to an agent beside the
// panel, which the panel hears about rather than the terminal losing focus.
func TestKeysMovingToAPaneTakeThemOffTheList(t *testing.T) {
	m, tmux := paneless(t)

	require.True(t, m.isListFocused(), "the panel has the keys to begin with")

	tmux.keysElsewhere = true
	left, _ := m.route(tea.BlurMsg{})
	m = asModel(t, left)

	assert.True(t, m.keysElsewhere, "the panel knows the keys went to a pane rather than out of the terminal")
	assert.False(t, m.isListFocused(), "so the list is no longer the thing being typed at")
}

// TestAConfiguredWidthIsWidenedAndCapped covers the width nobody has dragged: it is where the
// panel starts rather than a boundary the reader placed, so it is held to what a list of names
// reads in and to what leaves the block beside it its floor.
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
			m := sizedTo(infoModel(tmux), tmux)
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

// TestADraggedDividerIsDrawnWhereItIs covers the width the reader placed: the terminal underneath
// is split there, so the panel draws its own boundary there too rather than at a width the block
// beside it would rather have.
func TestADraggedDividerIsDrawnWhereItIs(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.enterScreen(screenReview)
	m.review = reviewScreen{
		agentID:   "a1",
		agentName: "auth",
		loading:   true,
	}

	// A divider dragged until the stage holds 25 columns, which leaves the block under its floor.
	tmux.width = 25
	m.frame.SyncSplit()

	split, ok := m.screenColumnSplit()
	require.True(t, ok)
	assert.Equal(t, 174, split.sidebar, "the window less the stage and the divider")
	assert.Equal(t, 26, split.column, "and the block keeps what is left rather than its floor")
}

// TestAWidenedWindowKeepsADraggedDivider covers a terminal made wider after the divider was
// dragged, where tmux shares the new columns out between both panes.
func TestAWidenedWindowKeepsADraggedDivider(t *testing.T) {
	tmux := &recordingTmux{}
	m := draggedTo(t, infoModel(tmux), tmux, 50, 149)

	dragged, measured := m.frame.SidebarWidth(200)
	require.True(t, measured, "the drag is followed while the window is the size it was made under")
	require.Equal(t, 50, dragged)

	// tmux widens both panes with the window, so each reads wider than it was dragged to.
	tmux.windowWidth = 300
	tmux.paneWidths["%0"] = 75
	tmux.paneWidths["%1"] = 224

	sized, _ := m.route(tea.WindowSizeMsg{Width: 75, Height: 40})
	m = asModel(t, sized)

	width, _ := m.frame.SidebarWidth(300)
	assert.Equal(t, 50, width, "the sidebar keeps the width it was dragged to")
	assert.Contains(t, tmux.resizes, "%0 50", "and tmux is told so")
}

// TestADraggedDividerSurvivesTheSidebarTakingTheWindow covers moving to a screen the panel draws
// whole, where the zoom reports the panel's pane as the window it was measured against.
func TestADraggedDividerSurvivesTheSidebarTakingTheWindow(t *testing.T) {
	tmux := &recordingTmux{}
	m := draggedTo(t, infoModel(tmux), tmux, 50, 149)

	m.frame.Solo(true)

	// The zoom makes the panel's own pane as wide as the window, which tmux reports as a resize.
	tmux.paneWidths["%0"] = 200

	sized, _ := m.route(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = asModel(t, sized)

	width, measured := m.frame.SidebarWidth(200)
	assert.True(t, measured, "the drag is still where the sidebar is drawn")
	assert.Equal(t, 50, width, "rather than the configured width")
}

// TestADraggedDividerSurvivesTmuxDroppingTheZoom covers tmux taking the sidebar's zoom away
// behind the frame's back, which the frame hears about as a resize and nothing else.
func TestADraggedDividerSurvivesTmuxDroppingTheZoom(t *testing.T) {
	tmux := &recordingTmux{}
	m := draggedTo(t, infoModel(tmux), tmux, 50, 149)

	m.frame.Solo(true)
	tmux.paneWidths["%0"] = 200

	sized, _ := m.route(tea.WindowSizeMsg{Width: 200, Height: 40})
	m = asModel(t, sized)

	// Moving the keys off the pane drops the zoom, and the frame is not told.
	tmux.paneWidths["%0"] = 50

	sized, _ = m.route(tea.WindowSizeMsg{Width: 50, Height: 40})
	m = asModel(t, sized)

	width, measured := m.frame.SidebarWidth(200)
	assert.True(t, measured, "the window never changed size, so the divider still stands")
	assert.Equal(t, 50, width)
}

// draggedTo puts the panel beside a live stage pane at the given widths and measures the divider
// there.
func draggedTo(t *testing.T, m model, tmux *recordingTmux, sidebar, stage int) model {
	t.Helper()

	m = sizedTo(m, tmux)
	tmux.paneWidths = map[string]int{"%0": sidebar, "%1": stage}

	sized, _ := m.route(tea.WindowSizeMsg{Width: sidebar, Height: 40})

	return asModel(t, sized)
}

// TestASplitAgentsListIsLaidOutAgainstItsOwnColumn covers the list the panel draws beside a
// document being fitted to the column it is drawn in rather than to the whole window.
func TestASplitAgentsListIsLaidOutAgainstItsOwnColumn(t *testing.T) {
	withColor(t)

	m, _ := paneless(t)
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

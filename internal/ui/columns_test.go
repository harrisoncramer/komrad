package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/engine"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/lib/xptr"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
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

// TestAgentsDrawTheHoveredRowsNotes covers a row with no pane of its own, whose notes are drawn in
// the panel's own frame now rather than paged into the stage.
func TestAgentsDrawTheHoveredRowsNotes(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.listing.workflows = []workflowstore.Workflow{{
		ID:      "w1",
		Name:    "backend",
		Context: "always run the linter",
	}}

	// The folder header, which has no pane and never did.
	m.tree.cursor = 0
	m.stageSelection()

	assert.True(t, m.frame.IsSolo(), "the panel takes the window when there is no pane to show")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "always run the linter", "the folder's shared notes stand beside the list")
	assert.Contains(t, view, "backend", "and the list is still there")

	for _, row := range strings.Split(m.View(), "\n") {
		assert.Equal(t, m.width, lipgloss.Width(row), "every row of the frame is squared off")
	}
}

// TestAgentsGiveTheWindowToARunningAgent covers the one row that has a pane of its own.
func TestAgentsGiveTheWindowToARunningAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)

	m.tree.cursor = 0
	m.stageSelection()
	require.True(t, m.frame.IsSolo(), "the folder header has no pane")

	// infoModel's one agent is alive with a pane of its own.
	m.tree.cursor = 1
	m.stageSelection()

	assert.False(t, m.frame.IsSolo(), "a running agent's own pane gets the window back")
	assert.Equal(t, m.width, m.paneWidth(), "and the list goes back to the whole of the pane")
	assert.Equal(t, 1, tmux.swaps, "the agent's pane is swapped into the stage")
}

// TestAgentsSayWhyADormantAgentHasNoPane covers the placeholders, which used to be a screenful of
// markdown pushed through a file into a pager.
func TestAgentsSayWhyADormantAgentHasNoPane(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.listing.agents = []agentItem{{agent: agentstore.Agent{
		ID:         "a1",
		Name:       "auth",
		WorkflowID: "w1",
	}}}

	m.tree.cursor = 1
	m.stageSelection()

	assert.True(t, m.frame.IsSolo(), "a dormant agent has no pane, so the panel keeps the window")
	assert.Contains(t, ansi.Strip(m.View()), "dormant", "and says so in its own frame")
}

// TestLeavingARunningAgentKeepsTheWindow covers moving off an agent onto a row with no pane, where
// the swap that puts the agent's pane back drops the zoom and left an empty pane beside the panel.
func TestLeavingARunningAgentKeepsTheWindow(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)

	m.tree.cursor = 1
	m.stageSelection()
	require.False(t, tmux.zoomed, "the agent's own pane has the stage to itself")

	m.tree.cursor = 0
	m.stageSelection()

	assert.True(t, m.frame.IsSolo(), "the panel means to hold the window on a row with no pane")
	assert.True(t, tmux.zoomed, "and tmux is holding it, rather than the frame only believing so")
}

// TestTheFrameCoversThePane covers the rows under a short frame, which used to keep whatever the
// taller frame before it had drawn there.
func TestTheFrameCoversThePane(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)

	// The running agent, whose own pane takes the window back, which is the short frame.
	m.tree.cursor = 1
	m.stageSelection()

	assert.Len(t, strings.Split(m.View(), "\n"), m.height, "the frame is as tall as the pane it is painted into")
}

// TestSchedulesDrawTheHoveredRecord covers the schedule's own record standing beside the list.
func TestSchedulesDrawTheHoveredRecord(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	tmux, ok := m.tmux.(*recordingTmux)
	require.True(t, ok)
	m = sizedTo(m, tmux)

	entered, _ := m.enterSchedules()
	m = asModel(t, entered)

	require.True(t, m.frame.IsSolo(), "the schedules screen draws both halves itself")

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "morning-ci", "the list names the schedule")
	assert.Contains(t, view, "0 9 * * 1-5", "and the record beside it spells out when it fires")
	assert.Empty(t, tmux.respawns, "nothing is pushed into a pane to put it there")
}

// paneless is the panel on a row with no pane of its own, which is where it holds the window.
func paneless(t *testing.T) (model, *recordingTmux) {
	t.Helper()

	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.listing.workflows = []workflowstore.Workflow{{
		ID:      "w1",
		Name:    "backend",
		Context: "notes",
	}}
	m.tree.cursor = 0
	m.stageSelection()

	require.True(t, m.frame.IsSolo(), "the folder header has no pane, so the panel holds the window")

	return m, tmux
}

// TestAHandMovedPaneCorrectsTheFrame covers a pane the user moved or zoomed themselves, which
// tmux's own prefix keys and mouse can still do inside the panel: the sidebar's zoom goes and
// nothing calls into weave, so the frame would otherwise draw both halves of a split into a pane
// that is sharing the window.
func TestAHandMovedPaneCorrectsTheFrame(t *testing.T) {
	m, tmux := paneless(t)

	tmux.keysElsewhere = true
	left, _ := m.route(tea.BlurMsg{})
	m = asModel(t, left)

	assert.False(t, m.frame.IsSolo(), "the frame is told what tmux did rather than assuming")
	assert.Equal(t, m.width, m.paneWidth(), "so the list is drawn in the share of the frame it has")

	// The panel wants the window on this row, so it takes it back on the next pass, which is the
	// behaviour before this branch and what #385 accepts by removing the key that fought it.
	tmux.keysElsewhere = false
	m.applyRefresh(refreshResult{})

	assert.True(t, m.frame.IsSolo(), "and takes it back on the next pass, since it wants it here")
}

// TestAnUnfocusedTerminalKeepsTheWindow covers the other thing a focus report means, which is
// the terminal itself losing focus with the zoom untouched.
func TestAnUnfocusedTerminalKeepsTheWindow(t *testing.T) {
	m, _ := paneless(t)
	before := m.View()

	// The keys have not moved, which is what separates this from the case above.
	left, _ := m.route(tea.BlurMsg{})
	m = asModel(t, left)

	assert.True(t, m.frame.IsSolo(), "the terminal losing focus is not the panel losing the window")
	assert.Equal(t, before, m.View(), "so nothing is re-laid out behind an unfocused terminal")
	assert.False(t, m.keysElsewhere, "and the panel is still the thing being read")
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

// TestTheDiffColumnSaysWhatItIsWaitingFor covers the review screen before its patch arrives,
// which on a large worktree can take seconds.
func TestTheDiffColumnSaysWhatItIsWaitingFor(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.enterScreen(screenReview)
	m.review = reviewState{
		agentID:   "a1",
		agentName: "auth",
		loading:   true,
	}

	split, ok := m.screenColumnSplit()
	require.True(t, ok, "the screen holds the window from the moment it is entered")
	require.Greater(t, split.column, split.sidebar, "and the diff has the larger half of it")

	column := ansi.Strip(m.reviewDiffView(split))
	assert.Contains(t, column, reviewLoadingWord, "which says what it is waiting for")
	// lipgloss rather than paint.CountLines, which takes one trailing newline off and so under-reports a
	// block whose last row is blank padding, as this one's is.
	assert.Equal(t, split.rows, lipgloss.Height(column), "and stands its full height while it waits")

	m.review.loading = false
	assert.Contains(t, ansi.Strip(m.reviewDiffView(split)), reviewEmptyWord,
		"a worktree that turned out to hold nothing says that instead")
}

// TestColumnOffsetResetsOnANewRow covers the scroll offset belonging to the row it was scrolled
// on.
func TestColumnOffsetResetsOnANewRow(t *testing.T) {
	var state columnState

	state.place("agent:a1")
	state.top = 24
	state.place("agent:a1")
	assert.Equal(t, 24, state.top, "staying on the row keeps the reader where they were")

	state.place("agent:a2")
	assert.Equal(t, 0, state.top, "a different row opens at its own top")
}

// TestSplitOfDrawsTheWidthItIsHanded covers the boundary being where the divider is: a measured
// width is the pane under the panel, so it is drawn there even where it leaves the sidebar or the
// block less than either would have asked for.
func TestSplitOfDrawsTheWidthItIsHanded(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		sidebar     int
		wantSidebar int
		wantOK      bool
	}{
		{
			name:        "a width in the middle of the range is drawn as it stands",
			width:       200,
			sidebar:     60,
			wantSidebar: 60,
			wantOK:      true,
		},
		{
			name:        "a width narrower than a list of names is still where the divider is",
			width:       200,
			sidebar:     12,
			wantSidebar: 12,
			wantOK:      true,
		},
		{
			name:        "so is one that leaves the block less than its floor",
			width:       100,
			sidebar:     80,
			wantSidebar: 80,
			wantOK:      true,
		},
		{
			name:        "a width past the window keeps a column for the block",
			width:       100,
			sidebar:     140,
			wantSidebar: 99,
			wantOK:      true,
		},
		{
			name:    "and a window too narrow for both halves holds neither",
			width:   60,
			sidebar: 30,
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			split, ok := splitOf(tt.width, 20, tt.sidebar, columnFloor)

			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}

			assert.Equal(t, tt.wantSidebar, split.sidebar)
			assert.Equal(t, tt.width-tt.wantSidebar, split.column)
		})
	}
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
			m.review = reviewState{
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
	m.review = reviewState{
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

// TestAgentWindowsAreTheSizeOfTheStage covers an agent's own window being kept at the size of the
// slot it is shown in.
func TestAgentWindowsAreTheSizeOfTheStage(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{
		ID:         "a2",
		Name:       "dormant",
		WorkflowID: "w1",
	}})

	m = resizedTo(t, m, 200, 40)

	assert.Equal(t, "200x40", tmux.windowSizes["%9"], "the running agent's own window is put at the stage's size")
	assert.NotContains(t, tmux.windowSizes, "", "and a dormant agent has no window to size")
}

// TestRemoteAgentWindowsAreLeftAlone covers a remote engine, whose agents' windows live on another
// tmux server where a pane id names something else, so nothing on this one is resized.
func TestRemoteAgentWindowsAreLeftAlone(t *testing.T) {
	tmux := &recordingTmux{}
	m := sizedTo(infoModel(tmux), tmux)
	m.frame = layout.New(tmux, config.DefaultLayout(), layout.WithEngine(engine.Remote{Exec: []string{"ssh", "engine"}}))
	m.frame.Attach("%0")

	m = resizedTo(t, m, 200, 40)

	assert.Empty(t, tmux.windowSizes, "no window on the panel's own server is touched")
}

// TestAWidenedWindowDropsADraggedDivider covers a terminal made wider after the divider was
// dragged, where keeping the stage's columns hands every new column to the sidebar.
func TestAWidenedWindowDropsADraggedDivider(t *testing.T) {
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

	width, measured := m.frame.SidebarWidth(300)
	assert.False(t, measured, "a window that changed size is not one the divider was measured under")
	assert.Equal(t, config.DefaultSidebarWidth, width, "so the configured width is used again")
	assert.NotEqual(t, 150, width, "rather than every column the window gained")
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
// document being fitted to the column it is drawn in rather than to the whole window, since a row
// laid out against the window is squared off by the frame afterwards and loses its marks before
// its name.
func TestASplitAgentsListIsLaidOutAgainstItsOwnColumn(t *testing.T) {
	withColor(t)

	m, _ := paneless(t)
	m.listing.agents = []agentItem{{agent: agentstore.Agent{
		ID:         "a1",
		Name:       strings.Repeat("long-agent-name-", 6),
		WorkflowID: "w1",
		State:      agentstore.StateIdle,
		ScheduleID: xptr.Ptr("s1"),
	}}}
	m.tree.selected = selectAgents([]string{"a1"}...)

	split, ok := m.screenColumnSplit()
	require.True(t, ok, "the hovered header has no pane, so the panel draws a document beside its list")

	sidebar := m.at(split.sidebar)
	require.Equal(t, split.sidebar, sidebar.paneWidth())

	for i, row := range strings.Split(strings.TrimRight(sidebar.agentsListView(), "\n"), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(row), split.sidebar,
			"row %d reaches past its own column: %q", i, ansi.Strip(row))
	}
}

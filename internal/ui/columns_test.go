package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sizedTo gives a model a window big enough to hold both halves and attaches its frame, which is
// what every screen drawing a block of its own needs before it will draw one.
func sizedTo(m model, tmux *recordingTmux) model {
	tmux.width, tmux.height = 200, 40
	m.width, m.height = 200, 40
	m.frame.Attach("%0")

	return m
}

// resizedTo puts the terminal and the panel's pane at the same size, since the panel reads its
// size back from the pane.
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

// TestAgentsGiveTheWindowToARunningAgent covers the one row that has a pane of its own, since the
// agent's own terminal is the whole point of the stage beside the list.
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

// TestMailDrawsTheHoveredMessage covers the body of a message being drawn beside the threads,
// which is what the stage pane was reading out of a file until now.
func TestMailDrawsTheHoveredMessage(t *testing.T) {
	m := mailModel(config.DefaultKeybindings())
	tmux, ok := m.tmux.(*recordingTmux)
	require.True(t, ok)
	m = sizedTo(m, tmux)
	// The model is built on the screen rather than navigated onto it, so nothing has asked for
	// the window yet.
	m.reconcileSolo()

	m.applyMailLoaded(mailLoadedMsg{
		agentID: "a1",
		inbox:   []*agentv1.Mail{received("1", "emacs-fan", "the linter is unhappy about the new file")},
	})

	require.True(t, m.frame.IsSolo(), "the mail screen draws both halves itself")
	assert.Contains(t, ansi.Strip(m.View()), "the linter is unhappy", "the message body stands beside the threads")
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

// TestAnUnfocusedTerminalKeepsTheWindow covers the other thing a focus report means, since tmux
// passes its client's own focus-out to the active pane and so alt-tabbing to another application
// looks exactly like the keys leaving while the zoom is untouched, measured as window_zoomed_flag
// 1 and pane_active 1 against 0 and 0 for a select-pane.
func TestAnUnfocusedTerminalKeepsTheWindow(t *testing.T) {
	m, _ := paneless(t)
	before := m.View()

	// The keys have not moved, which is what separates this from the case above.
	left, _ := m.route(tea.BlurMsg{})
	m = asModel(t, left)

	assert.True(t, m.frame.IsSolo(), "the terminal losing focus is not the panel losing the window")
	assert.Equal(t, before, m.View(), "so nothing is re-laid out behind an unfocused terminal")
}

// TestTheDiffColumnSaysWhatItIsWaitingFor covers the review screen before its patch arrives,
// which can be seconds on a large worktree and read as a screen that had hung, since the column
// is the larger half of the window and the band standing over it carries only the title.
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
	// lipgloss rather than countLines, which takes one trailing newline off and so under-reports a
	// block whose last row is blank padding, as this one's is.
	assert.Equal(t, split.rows, lipgloss.Height(column), "and stands its full height while it waits")

	m.review.loading = false
	assert.Contains(t, ansi.Strip(m.reviewDiffView(split)), reviewEmptyWord,
		"a worktree that turned out to hold nothing says that instead")
}

// TestColumnOffsetResetsOnANewRow covers the scroll offset belonging to the row it was scrolled
// on, since the offset a reader had is into a different document once the cursor moves.
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
// slot it is shown in, since a pane swapped into a slot of another size is resized by tmux and the
// CLI in it redraws its whole transcript.
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

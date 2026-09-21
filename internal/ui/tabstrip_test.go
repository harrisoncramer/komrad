package ui

import (
	"strings"
	"testing"
	"time"

	"weave/internal/ui/paint"
	"weave/lib/xslice"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stripFixture() tabStrip {
	return tabStrip{
		title:  "↑↓",
		legend: "sort",
		options: []tabOption{
			{id: "size", label: "size"},
			{id: "age", label: "age"},
			{id: "name", label: "name"},
		},
		fallback: "size",
	}
}

func withANSI(t *testing.T) {
	t.Helper()

	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.ANSI)
}

// TestTabStripDrawsTheChosenTabInTheTonesOfItsFocus covers the title always muted, the chosen tab
// carrying its arrow and muted like the rest while the list holds the keys, the focus drawn in
// the cursor's accent while the strip holds them, on the chosen tab or on whichever the focus
// has moved to.
func TestTabStripDrawsTheChosenTabInTheTonesOfItsFocus(t *testing.T) {
	withANSI(t)

	muted := paint.Panel.Style(paint.ToneMuted)
	accent := paint.Panel.Cursor()

	tests := []struct {
		name  string
		state tabsState
		want  string
	}{
		{
			name:  "the zero state is the fallback with its arrow down, on the list's keys",
			state: tabsState{},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name"),
		},
		{
			name:  "a chosen tab keeps its arrow when the list holds the keys",
			state: tabsState{chosen: "age", direction: tabUp},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("size") + "  " + muted.Render("↑ age") + "  " + muted.Render("name"),
		},
		{
			name:  "the strip holding the keys draws the chosen tab in the accent",
			state: tabsState{chosen: "age", focused: true},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("size") + "  " + accent.Render("↓ age") + "  " + muted.Render("name"),
		},
		{
			name:  "a focus moved off the chosen tab lights the tab it stands on and leaves the arrow",
			state: tabsState{chosen: "age", focused: true, focus: "name"},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("size") + "  " + muted.Render("↓ age") + "  " + accent.Render("name"),
		},
		{
			name:  "a choice the strip does not hold falls back",
			state: tabsState{chosen: "gone"},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripFixture().view(tt.state, paint.Panel, 0))
		})
	}
}

// TestTabStripIsCutToThePane covers a strip wider than the pane ending in an ellipsis rather than
// isRunning past it.
func TestTabStripIsCutToThePane(t *testing.T) {
	drawn := stripFixture().view(tabsState{}, paint.Panel, 12)

	assert.Equal(t, 12, lipgloss.Width(drawn))
	assert.True(t, strings.HasSuffix(drawn, "…"))
}

// TestTabStripStepsAndWraps covers tab and shift+tab walking the focus along the tabs and
// wrapping at both ends, with the chosen tab and its arrow left where they were.
func TestTabStripStepsAndWraps(t *testing.T) {
	tests := []struct {
		name  string
		from  tabsState
		delta int
		want  string
	}{
		{
			name:  "back from the first wraps to the last",
			from:  tabsState{chosen: "size"},
			delta: -1,
			want:  "name",
		},
		{
			name:  "forward from the last wraps to the first",
			from:  tabsState{chosen: "size", focus: "name"},
			delta: 1,
			want:  "size",
		},
		{
			name:  "forward steps to the next",
			from:  tabsState{chosen: "size", direction: tabUp},
			delta: 1,
			want:  "age",
		},
		{
			name:  "back steps to the previous",
			from:  tabsState{chosen: "age", focus: "name"},
			delta: -1,
			want:  "age",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.from

			stripFixture().step(&state, tt.delta)

			assert.Equal(t, tt.want, state.focus)
			assert.Equal(t, tt.from.chosen, state.chosen)
			assert.Equal(t, tt.from.direction, state.direction)
		})
	}
}

// TestTabStripChoosesTheFocusedTab covers a direction key sorting by the tab the focus stands
// on, in that direction, and the focus settling back onto the chosen tab.
func TestTabStripChoosesTheFocusedTab(t *testing.T) {
	state := tabsState{chosen: "size", focus: "name", focused: true}

	stripFixture().choose(&state, tabUp)

	assert.Equal(t, tabsState{chosen: "name", direction: tabUp, focused: true}, state)
}

func groupedStripFixture() tabStrip {
	strip := stripFixture()
	strip.grouped = "repo"

	return strip
}

// TestTabStripDrawsTheGroupStop covers the stop after the columns: muted with the grouped label
// while grouped, reading flat while not, lit in the accent only while the focus is on it, and
// the chosen column falling back to muted while it is.
func TestTabStripDrawsTheGroupStop(t *testing.T) {
	withANSI(t)

	muted := paint.Panel.Style(paint.ToneMuted)
	accent := paint.Panel.Cursor()
	columns := " " + muted.Render("↑↓") + "   " + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name")
	dot := "  " + muted.Render("·") + "  "

	tests := []struct {
		name  string
		state tabsState
		want  string
	}{
		{
			name:  "grouped on the list's keys",
			state: tabsState{},
			want:  columns + dot + muted.Render("repo"),
		},
		{
			name:  "flat on the list's keys",
			state: tabsState{flat: true},
			want:  columns + dot + muted.Render("flat"),
		},
		{
			name:  "the focus on a column leaves the stop muted",
			state: tabsState{focused: true, flat: true},
			want:  " " + muted.Render("↑↓") + "   " + accent.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("flat"),
		},
		{
			name:  "the focus on the stop lights it and not the column",
			state: tabsState{focused: true, onGroup: true},
			want:  columns + dot + accent.Render("repo"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, groupedStripFixture().view(tt.state, paint.Panel, 0))
		})
	}
}

// TestTabStripStepsOntoTheGroupStop covers tab walking the focus from the last column onto the
// stop and from the stop back to the first column, shift+tab the other way, and the chosen
// column and its arrow standing still throughout.
func TestTabStripStepsOntoTheGroupStop(t *testing.T) {
	tests := []struct {
		name  string
		from  tabsState
		delta int
		want  tabsState
	}{
		{
			name:  "forward from the last column lands on the stop",
			from:  tabsState{chosen: "size", focus: "name"},
			delta: 1,
			want:  tabsState{chosen: "size", onGroup: true},
		},
		{
			name:  "forward from the stop wraps to the first column",
			from:  tabsState{chosen: "name", direction: tabUp, onGroup: true, flat: true},
			delta: 1,
			want:  tabsState{chosen: "name", direction: tabUp, focus: "size", flat: true},
		},
		{
			name:  "back from the first column wraps to the stop",
			from:  tabsState{chosen: "size"},
			delta: -1,
			want:  tabsState{chosen: "size", onGroup: true},
		},
		{
			name:  "back from the stop lands on the last column",
			from:  tabsState{chosen: "age", direction: tabUp, onGroup: true},
			delta: -1,
			want:  tabsState{chosen: "age", direction: tabUp, focus: "name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.from

			groupedStripFixture().step(&state, tt.delta)

			assert.Equal(t, tt.want, state)
		})
	}
}

// TestTabsGroupStopFlipsFlat covers the navigation keys flipping the grouping while the focus is
// on the stop, with the chosen column and its arrow left alone by the walk there and by the
// flip, the cursor keeping its row across the flip, and escape handing the keys back with the
// grouping kept.
func TestTabsGroupStopFlipsFlat(t *testing.T) {
	m := sortedWorktreesModel()
	require.Equal(t, "tree:/trees/live", m.worktrees.list().hovered(m.shared()))

	onStop := send(t, send(t, send(t, m, "f"), "tab"), "tab")
	require.Equal(t, tabsState{focused: true, onGroup: true}, onStop.worktrees.sort, "the walk chooses nothing")

	flat := send(t, onStop, "j")
	assert.Equal(t, tabsState{focused: true, onGroup: true, flat: true}, flat.worktrees.sort)
	assert.Equal(t, "tree:/trees/live", flat.worktrees.list().hovered(flat.shared()), "the cursor keeps its row")
	assert.Equal(t, 2, flat.worktrees.cursor, "third in the one list by size, the tie with its twin settled by path")
	assert.Contains(t, flat.worktrees.tally(), "flat")

	grouped := send(t, flat, "k")
	assert.Equal(t, tabsState{focused: true, onGroup: true}, grouped.worktrees.sort)
	assert.Equal(t, "tree:/trees/live", grouped.worktrees.list().hovered(grouped.shared()))
	assert.Equal(t, 4, grouped.worktrees.cursor, "back under its heading")
	assert.NotContains(t, grouped.worktrees.tally(), "flat")

	left := send(t, send(t, grouped, "j"), "esc")
	assert.Equal(t, tabsState{flat: true}, left.worktrees.sort, "the grouping is kept and the focus leaves the stop")
	assert.True(t, left.isListFocused())

	back := send(t, send(t, left, "f"), "shift+tab")
	assert.Equal(t, tabsState{focused: true, onGroup: true, flat: true}, back.worktrees.sort, "the focus comes back on the chosen column, a step from the stop")
}

// TestBlurLeavesTheGroupStop covers the focus leaving the strip by any way but escape coming
// back on the chosen column rather than on the stop, so the next direction key turns the arrow.
func TestBlurLeavesTheGroupStop(t *testing.T) {
	m := sortedWorktreesModel()

	onStop := send(t, send(t, send(t, m, "f"), "tab"), "tab")
	require.True(t, onStop.worktrees.sort.onGroup)

	onStop.blurTabs()
	assert.Equal(t, tabsState{}, onStop.worktrees.sort)

	turned := send(t, send(t, onStop, "f"), "k")
	assert.Equal(t, tabsState{chosen: "size", direction: tabUp, focused: true}, turned.worktrees.sort)
}

// TestFlatWorktreesCursorLeavesAHeadingForARow covers a cursor on a heading landing on the
// nearest row once the headings are gone.
func TestFlatWorktreesCursorLeavesAHeadingForARow(t *testing.T) {
	m := sortedWorktreesModel()
	m.worktrees.cursor = 2
	require.Equal(t, "repo:/home/h/weave", m.worktrees.list().hovered(m.shared()))

	flat := send(t, send(t, send(t, send(t, m, "f"), "tab"), "tab"), "j")

	assert.True(t, flat.worktrees.sort.flat)
	row, ok := flat.worktrees.hovered(flat.shared())
	require.True(t, ok)
	assert.False(t, row.header)
	assert.Equal(t, 2, flat.worktrees.cursor)
}

// sortedWorktreesModel is the worktrees screen over the measured fixture, with the cursor on
// the live agent's directory.
func sortedWorktreesModel() model {
	m := worktreesModel()
	m.width, m.height = 200, 40
	m.worktrees.items = measuredFixture(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	m.worktrees.cursor = 4

	return m
}

// TestTabsFocusTakesTheKeys covers the focus key handing the keys to the strip, where tab and
// shift+tab move the focus without touching the order, the navigation keys sort by the focused
// column in their direction, the rows reordering under the cursor and the cursor staying on its
// row, the focus key doing nothing more once the strip has the keys, and escape handing them
// back with the choice kept.
func TestTabsFocusTakesTheKeys(t *testing.T) {
	m := sortedWorktreesModel()
	require.Equal(t, "tree:/trees/live", m.worktrees.list().hovered(m.shared()))
	before := xslice.Map(m.worktrees.rows(m.shared()), worktreeRow.key)

	focused := send(t, m, "f")
	require.True(t, focused.isTabsFocused())
	assert.False(t, focused.isListFocused(), "the row caret stands still")

	tabbed := send(t, focused, "tab")
	assert.Equal(t, tabsState{focused: true, focus: "last-accessed"}, tabbed.worktrees.sort)
	assert.Equal(t, before, xslice.Map(tabbed.worktrees.rows(tabbed.shared()), worktreeRow.key), "moving the focus reorders nothing")
	assert.Equal(t, 4, tabbed.worktrees.cursor)

	chosen := send(t, tabbed, "j")
	assert.Equal(t, tabsState{chosen: "last-accessed", direction: tabDown, focused: true}, chosen.worktrees.sort)
	assert.NotEqual(t, before, xslice.Map(chosen.worktrees.rows(chosen.shared()), worktreeRow.key), "the rows take the order the key names")
	assert.Equal(t, "tree:/trees/live", chosen.worktrees.list().hovered(chosen.shared()), "the cursor stays on its row")
	assert.Equal(t, 3, chosen.worktrees.cursor, "at the index the row moved to")

	turned := send(t, chosen, "k")
	assert.Equal(t, tabUp, turned.worktrees.sort.direction)
	assert.Equal(t, "tree:/trees/live", turned.worktrees.list().hovered(turned.shared()), "the cursor follows its row as the arrow turns")
	assert.Equal(t, 5, turned.worktrees.cursor)

	turned = send(t, turned, "j")
	assert.Equal(t, tabDown, turned.worktrees.sort.direction)
	assert.Equal(t, 3, turned.worktrees.cursor)

	pressed := send(t, turned, "f")
	require.True(t, pressed.isTabsFocused(), "the focus key is not the way back")
	assert.Equal(t, "last-accessed", pressed.worktrees.sort.chosen)

	back := send(t, pressed, "esc")
	require.False(t, back.isTabsFocused())
	assert.Equal(t, tabsState{chosen: "last-accessed", direction: tabDown}, back.worktrees.sort, "the choice is kept")
	assert.True(t, back.isListFocused())

	moved := send(t, back, "j")
	assert.Equal(t, 4, moved.worktrees.cursor, "the list has the keys again")
	assert.Equal(t, "tree:/trees/orphan", moved.worktrees.list().hovered(moved.shared()))
}

// TestEscapeLeavesTheTabsAndKeepsTheSort covers escape handing the keys back with the chosen
// column and its arrow as they were, from a focus that was wrapped to past the group stop, the
// rows staying in that order and the cursor on its row.
func TestEscapeLeavesTheTabsAndKeepsTheSort(t *testing.T) {
	m := sortedWorktreesModel()

	turned := send(t, send(t, send(t, send(t, m, "f"), "shift+tab"), "shift+tab"), "k")
	require.Equal(t, tabsState{chosen: "last-accessed", direction: tabUp, focused: true}, turned.worktrees.sort)
	require.Equal(t, 5, turned.worktrees.cursor, "the sort has moved the row")
	order := xslice.Map(turned.worktrees.rows(turned.shared()), worktreeRow.key)

	left := send(t, turned, "esc")

	assert.False(t, left.isTabsFocused())
	assert.Equal(t, tabsState{chosen: "last-accessed", direction: tabUp}, left.worktrees.sort)
	assert.Equal(t, order, xslice.Map(left.worktrees.rows(left.shared()), worktreeRow.key), "the rows keep the order the strip named")
	assert.Equal(t, screenWorktrees, left.screen, "the press is spent on the strip rather than on the screen's back key")
	assert.Equal(t, "tree:/trees/live", left.worktrees.list().hovered(left.shared()), "the cursor stays on its row")
	assert.Equal(t, 5, left.worktrees.cursor)
}

// TestTabsFocusAndTheRecordTakeTurns covers the record's focus key letting the strip go and the
// strip's letting the record go, so the keys are only ever in one place.
func TestTabsFocusAndTheRecordTakeTurns(t *testing.T) {
	m := sortedWorktreesModel()

	onRecord := send(t, m, "ctrl+t")
	require.True(t, onRecord.isDetailsFocused())

	onTabs := send(t, onRecord, "f")
	assert.True(t, onTabs.isTabsFocused())
	assert.False(t, onTabs.isDetailsFocused())

	backOnRecord := send(t, send(t, onTabs, "esc"), "ctrl+t")
	assert.True(t, backOnRecord.isDetailsFocused())
	assert.False(t, backOnRecord.isTabsFocused())
}

// TestTabsLegendWhileTheStripHoldsTheKeys covers the legend swapping to the strip's own keys and
// back, and the list's legend naming the focus key.
func TestTabsLegendWhileTheStripHoldsTheKeys(t *testing.T) {
	m := sortedWorktreesModel()

	list := drawnKeys(m.worktreesHelp())
	assert.Contains(t, list, "f")

	focused := send(t, m, "f")
	legend := focused.renderHelp(focused.worktreesHelp(), focused.worktreesChrome().header(focused))
	assert.Contains(t, legend, "move")
	assert.Contains(t, legend, "sort down/up, flip")
	assert.Contains(t, legend, "back to the list")
	assert.NotContains(t, legend, "jump to agent")

	focused.helpExpanded = true
	expanded := focused.renderHelp(focused.worktreesHelp(), focused.worktreesChrome().header(focused))
	assert.Contains(t, expanded, "tab/S-tab")
	assert.Contains(t, expanded, "hide help")
}

// TestTabsStandDownOnAShortPane covers the strip giving way at the height it would leave the list
// no row, standing again one row taller, and refusing the keys while it is not drawn.
func TestTabsStandDownOnAShortPane(t *testing.T) {
	m := screensModel()
	m.screen = screenWorktrees

	m.height = 6
	require.NotContains(t, m.worktreesChrome().header(m), "↑↓", "the strip gives up its row")
	assert.Contains(t, m.View(), "agent39", "to the row the cursor stands on")

	refused := send(t, m, "f")
	assert.False(t, refused.isTabsFocused(), "a strip that is not drawn takes no keys")
	assert.True(t, refused.isListFocused())

	refused.height = 7
	assert.False(t, refused.isTabsFocused(), "and the refused press does not wake once the pane grows")

	m.height = 7
	require.Contains(t, m.worktreesChrome().header(m), "↑↓", "the strip is drawn once the list keeps a row beside it")
	assert.Contains(t, m.View(), "agent39")
	assert.Len(t, lines(m.View()), 7)

	focused := send(t, m, "f")
	require.True(t, focused.isTabsFocused())

	shrunk := asModel(t, resize(focused, 80, 6))
	assert.False(t, shrunk.isTabsFocused(), "the keys go back to the list with the strip")
	assert.True(t, shrunk.isListFocused())

	grown := asModel(t, resize(shrunk, 80, 7))
	assert.False(t, grown.isTabsFocused(), "and stay there once the pane grows back")
	assert.Equal(t, 39, send(t, grown, "k").worktrees.cursor, "so the next press moves the list")
}

// TestTabsTakeOneRowOffTheList covers the list window shrinking by the strip's row and no more.
func TestTabsTakeOneRowOffTheList(t *testing.T) {
	m := screensModel()
	m.screen, m.height = screenWorktrees, 30

	with := m.listLines(m.worktreesChrome().header(m), m.renderHelp(m.worktreesHelp(), m.worktreesChrome().header(m)))
	without := m.listLines(m.talliedHeader("Worktrees", m.worktrees.tally()), m.renderHelp(m.worktreesHelp(), m.worktreesChrome().header(m)))

	assert.Equal(t, without-1, with)
}

// TestTabsSitUnderTheFilterLine covers the strip's place in the header: straight under the search
// line, with one blank row before the list.
func TestTabsSitUnderTheFilterLine(t *testing.T) {
	m := sortedWorktreesModel()

	header := strings.Split(strings.TrimSuffix(m.worktreesChrome().header(m), "\n"), "\n")

	require.Len(t, header, 5)
	assert.Contains(t, header[2], "/")
	assert.Contains(t, header[3], "↑↓")
	assert.Empty(t, header[4])
}

// TestWorktreesScreenGolden draws the sidebar whole, with the strip on the list's keys and on
// its own.
func TestWorktreesScreenGolden(t *testing.T) {
	withANSI(t)

	tests := []struct {
		name  string
		state tabsState
	}{
		{
			name:  "worktrees-sort-strip",
			state: tabsState{},
		},
		{
			name:  "worktrees-sort-strip-focused",
			state: tabsState{chosen: "last-accessed", direction: tabUp, focused: true},
		},
		{
			name:  "worktrees-sort-strip-flat",
			state: tabsState{flat: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			m.width, m.height = 44, 28
			m = m.measuredAt(44)
			m.worktrees.cursor = 4
			m.worktrees.sort = tt.state
			m.placeFilteredCursor("tree:/trees/live")

			requireGolden(t, tt.name, m.screenListView(m.worktrees.listParams(m.shared())))
		})
	}
}

// TestEveryTabbedScreenTakesTheStrip covers the three list screens that draw a strip taking the
// same keys: the focus key, tab onto the next tab, j choosing it and k turning the arrow, escape
// handing the keys back with the choice kept, and the strip sitting straight under the filter
// line on each.
func TestEveryTabbedScreenTakesTheStrip(t *testing.T) {
	tests := []struct {
		name   string
		screen screen
		state  func(*model) *tabsState
		second string
	}{
		{
			name:   "the agents tree",
			screen: screenAgents,
			state:  func(m *model) *tabsState { return &m.agents.sort },
			second: "activity",
		},
		{
			name:   "the archive",
			screen: screenArchive,
			state:  func(m *model) *tabsState { return &m.archive.sort },
			second: "name",
		},
		{
			name:   "the worktrees",
			screen: screenWorktrees,
			state:  func(m *model) *tabsState { return &m.worktrees.sort },
			second: "last-accessed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := screensModel()
			m.screen, m.height = tt.screen, 40

			drawn := lines(m.active().view(m))
			assert.Contains(t, drawn[2], "/")
			assert.Contains(t, drawn[3], "↑↓")
			assert.Empty(t, drawn[4])

			focused := send(t, m, "f")
			require.True(t, focused.isTabsFocused())
			assert.False(t, focused.isListFocused())

			turned := send(t, send(t, send(t, focused, "tab"), "j"), "k")
			assert.Equal(t, tabsState{chosen: tt.second, direction: tabUp, focused: true}, *tt.state(&turned))
			assert.Contains(t, turned.active().view(turned), "↑ "+tt.second)

			left := send(t, turned, "esc")
			assert.Equal(t, tabsState{chosen: tt.second, direction: tabUp}, *tt.state(&left))
			assert.Equal(t, tt.screen, left.screen)
			assert.True(t, left.isListFocused())
		})
	}
}

// TestAgentsAndArchiveScreenGolden draws the two sidebars whole with their strips, on the list's
// keys and on their own.
func TestAgentsAndArchiveScreenGolden(t *testing.T) {
	withANSI(t)

	tests := []struct {
		name   string
		screen screen
		state  tabsState
	}{
		{
			name:   "agents-sort-strip",
			screen: screenAgents,
			state:  tabsState{},
		},
		{
			name:   "agents-sort-strip-focused",
			screen: screenAgents,
			state:  tabsState{chosen: "activity", focused: true},
		},
		{
			name:   "archive-sort-strip",
			screen: screenArchive,
			state:  tabsState{},
		},
		{
			name:   "archive-sort-strip-focused",
			screen: screenArchive,
			state:  tabsState{chosen: "name", direction: tabUp, focused: true},
		},
		{
			name:   "agents-sort-strip-flat",
			screen: screenAgents,
			state:  tabsState{chosen: "activity", flat: true},
		},
		{
			name:   "archive-sort-strip-flat",
			screen: screenArchive,
			state:  tabsState{flat: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			m.width, m.height = 44, 28
			m = m.measuredAt(44)
			m.screen = tt.screen
			m.agents.cursor = 1
			m.agents.sort, m.archive.sort = tt.state, tt.state

			requireGolden(t, tt.name, m.active().view(m))
		})
	}
}

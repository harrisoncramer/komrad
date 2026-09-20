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
// carrying its arrow and drawn plain while the list holds the keys and in the cursor's accent
// while the strip does, and every other tab muted.
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
			want:  " " + muted.Render("↑↓") + "   ↓ size  " + muted.Render("age") + "  " + muted.Render("name"),
		},
		{
			name:  "a chosen tab keeps its arrow when the list holds the keys",
			state: tabsState{chosen: "age", direction: tabUp},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("size") + "  ↑ age  " + muted.Render("name"),
		},
		{
			name:  "the strip holding the keys draws the chosen tab in the accent",
			state: tabsState{chosen: "age", focused: true},
			want:  " " + muted.Render("↑↓") + "   " + muted.Render("size") + "  " + accent.Render("↓ age") + "  " + muted.Render("name"),
		},
		{
			name:  "a choice the strip does not hold falls back",
			state: tabsState{chosen: "gone"},
			want:  " " + muted.Render("↑↓") + "   ↓ size  " + muted.Render("age") + "  " + muted.Render("name"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripFixture().view(tt.state, paint.Panel, 0))
		})
	}
}

// TestTabStripIsCutToThePane covers a strip wider than the pane ending in an ellipsis rather than
// running past it.
func TestTabStripIsCutToThePane(t *testing.T) {
	drawn := stripFixture().view(tabsState{}, paint.Panel, 12)

	assert.Equal(t, 12, lipgloss.Width(drawn))
	assert.True(t, strings.HasSuffix(drawn, "…"))
}

// TestTabStripStepsAndWraps covers tab and shift+tab walking the tabs and wrapping at both ends,
// and the choice being what selected reports.
func TestTabStripStepsAndWraps(t *testing.T) {
	tests := []struct {
		name  string
		from  string
		delta int
		want  string
	}{
		{
			name:  "back from the first wraps to the last",
			from:  "size",
			delta: -1,
			want:  "name",
		},
		{
			name:  "forward from the last wraps to the first",
			from:  "name",
			delta: 1,
			want:  "size",
		},
		{
			name:  "forward steps to the next",
			from:  "size",
			delta: 1,
			want:  "age",
		},
		{
			name:  "back steps to the previous",
			from:  "name",
			delta: -1,
			want:  "age",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strip := stripFixture()
			state := tabsState{chosen: tt.from}

			strip.step(&state, tt.delta)

			assert.Equal(t, tt.want, state.chosen)
		})
	}
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
// shift+tab move the choice and the navigation keys turn its arrow, the rows reordering under
// the cursor and the cursor staying on its row, and the focus key handing them back with the
// choice kept.
func TestTabsFocusTakesTheKeys(t *testing.T) {
	m := sortedWorktreesModel()
	require.Equal(t, "tree:/trees/live", m.hoveredWorktreeKey())
	before := xslice.Map(m.worktreeRows(), worktreeRow.key)

	focused := send(t, m, "f")
	require.True(t, focused.isTabsFocused())
	assert.False(t, focused.isListFocused(), "the row caret stands still")

	tabbed := send(t, focused, "tab")
	assert.Equal(t, "last-accessed", tabbed.worktrees.sort.chosen)
	assert.Equal(t, tabDown, tabbed.worktrees.sort.direction)
	assert.NotEqual(t, before, xslice.Map(tabbed.worktreeRows(), worktreeRow.key), "the rows take the order the tab names")
	assert.Equal(t, "tree:/trees/live", tabbed.hoveredWorktreeKey(), "the cursor stays on its row")
	assert.Equal(t, 3, tabbed.worktrees.cursor, "at the index the row moved to")

	turned := send(t, tabbed, "k")
	assert.Equal(t, tabUp, turned.worktrees.sort.direction)
	assert.Equal(t, "tree:/trees/live", turned.hoveredWorktreeKey(), "the cursor follows its row as the arrow turns")
	assert.Equal(t, 5, turned.worktrees.cursor)

	turned = send(t, turned, "j")
	assert.Equal(t, tabDown, turned.worktrees.sort.direction)
	assert.Equal(t, 3, turned.worktrees.cursor)

	back := send(t, turned, "f")
	require.False(t, back.isTabsFocused())
	assert.Equal(t, "last-accessed", back.worktrees.sort.chosen, "the choice is kept")
	assert.True(t, back.isListFocused())

	moved := send(t, back, "j")
	assert.Equal(t, 4, moved.worktrees.cursor, "the list has the keys again")
	assert.Equal(t, "tree:/trees/orphan", moved.hoveredWorktreeKey())
}

// TestEscapeLeavesTheTabsAndResetsThem covers escape handing the keys back with the fallback
// chosen and its arrow down again, doing so from a choice that was wrapped to, and the cursor
// staying on its row through the reset.
func TestEscapeLeavesTheTabsAndResetsThem(t *testing.T) {
	m := sortedWorktreesModel()

	turned := send(t, send(t, send(t, m, "f"), "shift+tab"), "k")
	require.Equal(t, tabsState{chosen: "last-accessed", direction: tabUp, focused: true}, turned.worktrees.sort)
	require.Equal(t, 5, turned.worktrees.cursor, "the sort has moved the row")

	left := send(t, turned, "esc")

	assert.False(t, left.isTabsFocused())
	assert.Equal(t, tabsState{}, left.worktrees.sort)
	assert.Equal(t, screenWorktrees, left.screen, "the press is spent on the strip rather than on the screen's back key")
	assert.Equal(t, "tree:/trees/live", left.hoveredWorktreeKey(), "the cursor stays on its row")
	assert.Equal(t, 4, left.worktrees.cursor)
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

	backOnRecord := send(t, send(t, onTabs, "f"), "ctrl+t")
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
	legend := focused.renderHelp(focused.worktreesHelp(), focused.worktreesHeader())
	assert.Contains(t, legend, "column")
	assert.Contains(t, legend, "direction")
	assert.Contains(t, legend, "back to the list")
	assert.NotContains(t, legend, "jump to agent")

	focused.helpExpanded = true
	expanded := focused.renderHelp(focused.worktreesHelp(), focused.worktreesHeader())
	assert.Contains(t, expanded, "tab/S-tab")
	assert.Contains(t, expanded, "hide help")
}

// TestTabsStandDownOnAShortPane covers the strip giving way at the height it would leave the list
// no row, standing again one row taller, and refusing the keys while it is not drawn.
func TestTabsStandDownOnAShortPane(t *testing.T) {
	m := screensModel()
	m.screen = screenWorktrees

	m.height = 6
	require.NotContains(t, m.worktreesHeader(), "↑↓", "the strip gives up its row")
	assert.Contains(t, m.View(), "agent39", "to the row the cursor stands on")

	refused := send(t, m, "f")
	assert.False(t, refused.isTabsFocused(), "a strip that is not drawn takes no keys")
	assert.True(t, refused.isListFocused())

	refused.height = 7
	assert.False(t, refused.isTabsFocused(), "and the refused press does not wake once the pane grows")

	m.height = 7
	require.Contains(t, m.worktreesHeader(), "↑↓", "the strip is drawn once the list keeps a row beside it")
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

	with := m.listLines(m.worktreesHeader(), m.renderHelp(m.worktreesHelp(), m.worktreesHeader()))
	without := m.listLines(m.talliedHeader("Worktrees", m.worktreesTally()), m.renderHelp(m.worktreesHelp(), m.worktreesHeader()))

	assert.Equal(t, without-1, with)
}

// TestTabsSitUnderTheFilterLine covers the strip's place in the header: straight under the search
// line, with one blank row before the list.
func TestTabsSitUnderTheFilterLine(t *testing.T) {
	m := sortedWorktreesModel()

	header := strings.Split(strings.TrimSuffix(m.worktreesHeader(), "\n"), "\n")

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			m.width, m.height = 44, 28
			m = m.at(44)
			m.worktrees.cursor = 4
			m.worktrees.sort = tt.state

			requireGolden(t, tt.name, m.worktreesListView())
		})
	}
}

// TestEveryTabbedScreenTakesTheStrip covers the three list screens that draw a strip taking the
// same keys: the focus key, tab onto the next tab, k turning the arrow, escape resetting, and the
// strip sitting straight under the filter line on each.
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
			state:  func(m *model) *tabsState { return &m.tree.sort },
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

			drawn := lines(m.screen.spec().view(m))
			assert.Contains(t, drawn[2], "/")
			assert.Contains(t, drawn[3], "↑↓")
			assert.Empty(t, drawn[4])

			focused := send(t, m, "f")
			require.True(t, focused.isTabsFocused())
			assert.False(t, focused.isListFocused())

			turned := send(t, send(t, focused, "tab"), "k")
			assert.Equal(t, tabsState{chosen: tt.second, direction: tabUp, focused: true}, *tt.state(&turned))
			assert.Contains(t, turned.screen.spec().view(turned), "↑ "+tt.second)

			left := send(t, turned, "esc")
			assert.Equal(t, tabsState{}, *tt.state(&left))
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			m.width, m.height = 44, 28
			m = m.at(44)
			m.screen = tt.screen
			m.tree.cursor = 1
			m.tree.sort, m.archive.sort = tt.state, tt.state

			requireGolden(t, tt.name, m.screen.spec().view(m))
		})
	}
}

package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/paint"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withColour(t *testing.T) {
	t.Helper()

	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
}

// TestListSearchLightsTheRecord covers the one search reaching both sections: typed on the list,
// it lights its matches in the record too, and a filter narrows the list alone.
func TestListSearchLightsTheRecord(t *testing.T) {
	withColour(t)

	m := detailsModel(&recordingTmux{})

	typed := pressMark(t, m, "/", "a", "u", "t", "h")
	require.True(t, typed.filter.typing)
	assert.Equal(t, sectionList, typed.filter.over)

	held := typed.hoveredDetails()
	rows := typed.detailFieldRows(typed.detailsRows(held), typed.detailsCursor(held))
	name := rows[indexOf(fieldLabels(held), "name")]
	assert.Contains(t, name, paint.Panel.Match().Render("auth"), "the record's match is lit")

	narrowed := pressMark(t, typed, "tab", "enter")
	require.True(t, narrowed.filter.doesNarrow(sectionList))
	assert.False(t, narrowed.filter.doesNarrow(sectionRecord))
	assert.Len(t, narrowed.detailsRows(narrowed.hoveredDetails()), len(held.fields), "the record keeps every field")
}

// TestFocusKeyWhileTypingJumpsIntoTheRecord covers the focus key answering while the search input
// is open: the input closes with the pattern standing, the record takes the keys, and its cursor
// lands on the first field the search matches.
func TestFocusKeyWhileTypingJumpsIntoTheRecord(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	jumped := pressMark(t, m, "/", "a", "b", "c", "ctrl+t")

	require.False(t, jumped.filter.typing, "the input is closed")
	assert.True(t, jumped.filter.isStanding(), "and the pattern stands")
	require.True(t, jumped.isDetailsFocused())

	held := jumped.hoveredDetails()
	assert.Equal(t, "session_id", held.fields[jumped.detailsCursor(held)].label)

	back := pressMark(t, jumped, "/", "p", "r", "ctrl+t")

	assert.False(t, back.isDetailsFocused(), "the key moves the keys the other way too")
	assert.False(t, back.filter.typing)
	assert.Equal(t, "pr", back.filter.query)
	assert.Equal(t, 1, back.tree.cursor, "without moving the list's cursor")
}

// TestTheEdgesScrollThrough covers the down key on the last row of the list walking into the
// record, and the up key on its first field walking back out, with the list's cursor where it was.
func TestTheEdgesScrollThrough(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	require.Equal(t, 1, m.tree.cursor, "the cursor starts on the last row")

	in := send(t, m, "j")
	require.True(t, in.isDetailsFocused())
	assert.Equal(t, 0, in.detailsCursor(in.hoveredDetails()))

	down := send(t, in, "j")
	assert.Equal(t, 1, down.detailsCursor(down.hoveredDetails()), "a second press moves within the record")

	out := pressMark(t, down, "k", "k")
	assert.False(t, out.isDetailsFocused())
	assert.Equal(t, 1, out.tree.cursor)

	up := send(t, out, "k")
	assert.Equal(t, 0, up.tree.cursor, "and the list moves again from there")
}

// TestTheEdgesScrollThroughOnTheSchedules covers the same walk on a list with a cursor of its own.
func TestTheEdgesScrollThroughOnTheSchedules(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	m.height, m.width = 24, 40
	m.detailsHeight = config.DefaultDetailsHeight

	bottom := send(t, m, "G")
	in := send(t, bottom, "j")
	require.True(t, in.isDetailsFocused())

	out := send(t, in, "k")
	assert.False(t, out.isDetailsFocused())
	assert.Equal(t, bottom.schedules.cursor, out.schedules.cursor)
}

// TestClearingASearchFromTheOtherSectionLeavesTheCursor covers escape over a standing search that
// was opened in the other section: the search goes, and the cursor the search walked stays put.
func TestClearingASearchFromTheOtherSectionLeavesTheCursor(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	walked := pressMark(t, m, "/", "b", "a", "c", "k", "enter")
	require.Equal(t, 0, walked.tree.cursor, "the search walked onto the workflow header")

	fromList := send(t, walked, "esc")
	assert.Equal(t, 1, fromList.tree.cursor, "cleared where it opened, the cursor goes back")

	fromRecord := pressMark(t, walked, "ctrl+t", "esc")
	assert.False(t, fromRecord.filter.isStanding())
	assert.Equal(t, 0, fromRecord.tree.cursor, "cleared from the record, the list stays where it is")
	assert.True(t, fromRecord.isDetailsFocused())
}

// TestRecordLabelTakesTheAccentWithTheKeys covers the hovered label drawn the way the lists draw
// a hovered name once the record has the keys, and dim like the rest while it does not.
func TestRecordLabelTakesTheAccentWithTheKeys(t *testing.T) {
	withColour(t)

	m := detailsModel(&recordingTmux{})
	held := m.hoveredDetails()
	label := padRight(held.fields[0].label, labelColumn(held.fields))

	blurred := m.detailFieldRows(m.detailsRows(held), 0)[0]
	assert.Contains(t, blurred, dimStyle.Render(label))

	m.details.focused = true
	require.True(t, m.isDetailsFocused())

	lit := m.detailFieldRows(m.detailsRows(held), 0)[0]
	assert.Contains(t, lit, rowCursorStyle().Render(label))
	assert.Equal(t, 1, strings.Count(ansi.Strip(lit), "›"))
}

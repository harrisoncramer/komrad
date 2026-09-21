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
// it lights its matches in the record too, and a filter narrows both once applied.
func TestListSearchLightsTheRecord(t *testing.T) {
	withColour(t)

	m := detailsModel(&recordingTmux{})

	typed := pressMark(t, m, "/", "a", "u", "t", "h")
	require.True(t, typed.filter.typing)

	held := typed.hoveredDetails()
	rows := typed.detailFieldRows(typed.detailsRows(held), typed.detailsCursor(held))
	name := rows[indexOf(fieldLabels(held), "name")]
	assert.Contains(t, name, paint.Panel.Match().Render("auth"), "the record's match is lit")

	narrowed := pressMark(t, typed, "tab", "enter")
	require.True(t, narrowed.filter.isNarrowing())
	assert.Equal(t, []string{"backend", "auth"}, rowNames(narrowed.agentRows()))
	assert.Len(t, narrowed.detailsRows(narrowed.hoveredDetails()), 1, "the record narrows to its matching field")
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

// TestRecordPagesItsFields covers the page keys moving the field cursor ten rows at a time,
// clamped to the record's ends.
func TestRecordPagesItsFields(t *testing.T) {
	m := recordSearchModel(t)
	held := m.hoveredDetails()
	require.Greater(t, len(held.fields), navPageStep+1)

	down := send(t, m, "ctrl+d")
	assert.Equal(t, navPageStep, down.detailsCursor(held))

	up := send(t, down, "ctrl+u")
	assert.Equal(t, 0, up.detailsCursor(held))

	bottom := pressMark(t, m, "G", "ctrl+d")
	assert.Equal(t, len(held.fields)-1, bottom.detailsCursor(held), "a page past the end stays on the last field")
	assert.True(t, bottom.isDetailsFocused(), "and keeps the keys")
}

// TestMatchKeysWalkAcrossTheSections covers n and N running one sequence over the list's rows
// and then the hovered record's: past the list's last match the keys go into the record, past
// the record's last match they come round to the list's first, and N walks it backwards.
func TestMatchKeysWalkAcrossTheSections(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.tree.cursor = 0

	standing := pressMark(t, m, "/", "b", "a", "c", "k", "enter")
	require.Equal(t, 0, standing.tree.cursor, "the header backend is the list's only match")
	require.False(t, standing.isDetailsFocused())

	into := send(t, standing, "n")
	require.True(t, into.isDetailsFocused(), "the next match is in the record")
	held := into.hoveredDetails()
	assert.Equal(t, "backend", held.fields[into.detailsCursor(held)].value)

	round := send(t, into, "n")
	assert.False(t, round.isDetailsFocused(), "past the record's last match the keys come round to the list")
	assert.Equal(t, 0, round.tree.cursor)

	back := send(t, round, "N")
	assert.True(t, back.isDetailsFocused(), "N walks the same sequence the other way")
	assert.Equal(t, "backend", held.fields[back.detailsCursor(held)].value)
}

// TestTypingMovesNothingUntilApplied covers the search lighting both sections while it is typed
// and acting only where it is applied: enter walks the list, and ctrl+t leaves the list where it
// was and walks the record instead, in either mode.
func TestTypingMovesNothingUntilApplied(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	require.Equal(t, 1, m.tree.cursor)

	typed := pressMark(t, m, "/", "b", "a", "c", "k")
	assert.Equal(t, 1, typed.tree.cursor, "the header matches and the cursor stays put")

	entered := send(t, typed, "enter")
	assert.Equal(t, 0, entered.tree.cursor, "enter walks onto the header")

	jumped := send(t, typed, "ctrl+t")
	require.True(t, jumped.isDetailsFocused())
	assert.Equal(t, 1, jumped.tree.cursor, "ctrl+t leaves the list alone")
	held := jumped.hoveredDetails()
	assert.Equal(t, "workflow", held.fields[jumped.detailsCursor(held)].label, "and lands on the record's match")

	filtered := pressMark(t, typed, "tab", "ctrl+t")
	require.True(t, filtered.isDetailsFocused())
	assert.Equal(t, 1, filtered.tree.cursor, "the list narrows around the row the cursor is on")
	assert.Len(t, filtered.agentRows(), len(m.agentRows()), "a header match keeps the rows under it")
	assert.Len(t, filtered.detailsRows(filtered.hoveredDetails()), 1, "and the record narrows with it")
}

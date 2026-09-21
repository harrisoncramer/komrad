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

// TestListSearchLightsTheRecord covers the one search lighting both panes while acting in one:
// typed on the list, its matches light in the record too, and a filter narrows the list alone.
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
	require.True(t, narrowed.filter.doesNarrow(sectionList))
	assert.False(t, narrowed.filter.doesNarrow(sectionRecord))
	assert.Equal(t, []string{"backend", "auth"}, rowNames(narrowed.agents.rows(narrowed.shared())))
	assert.Len(t, narrowed.detailsRows(narrowed.hoveredDetails()), len(held.fields), "the record keeps every field")
}

// TestFocusKeyWhileTypingJumpsIntoTheRecord covers the focus key as the one jump across panes:
// with a search typed on the list it moves the keys into the record and lands on the record's
// first match, and on the way back out it only moves the keys.
func TestFocusKeyWhileTypingJumpsIntoTheRecord(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	jumped := pressMark(t, m, "/", "a", "b", "c", "ctrl+t")

	require.False(t, jumped.filter.typing, "the input is closed")
	assert.True(t, jumped.filter.isStanding(), "and the pattern stands")
	require.True(t, jumped.isDetailsFocused())
	assert.Equal(t, 1, jumped.agents.cursor, "the list never moved")

	held := jumped.hoveredDetails()
	assert.Equal(t, "session_id", held.fields[jumped.detailsCursor(held)].label, "the record's cursor is on the match")

	back := send(t, jumped, "ctrl+t")
	assert.False(t, back.isDetailsFocused())
	assert.Equal(t, 1, back.agents.cursor, "the way out moves nothing")

	standing := pressMark(t, back, "/", "b", "a", "c", "k", "enter", "ctrl+t")
	require.True(t, standing.isDetailsFocused(), "a standing search jumps the same way")
	assert.Equal(t, 0, standing.agents.cursor, "enter walked the list onto the header first")
	header := standing.hoveredDetails()
	assert.Equal(t, "name", header.fields[standing.detailsCursor(header)].label, "and the jump lands on the header's own match")
}

// TestTheEdgesScrollThrough covers the down key on the last row of the list walking into the
// record, and the up key on its first field walking back out, with the list's cursor where it was.
func TestTheEdgesScrollThrough(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	require.Equal(t, 1, m.agents.cursor, "the cursor starts on the last row")

	in := send(t, m, "j")
	require.True(t, in.isDetailsFocused())
	assert.Equal(t, 0, in.detailsCursor(in.hoveredDetails()))

	down := send(t, in, "j")
	assert.Equal(t, 1, down.detailsCursor(down.hoveredDetails()), "a second press moves within the record")

	out := pressMark(t, down, "k", "k")
	assert.False(t, out.isDetailsFocused())
	assert.Equal(t, 1, out.agents.cursor)

	up := send(t, out, "k")
	assert.Equal(t, 0, up.agents.cursor, "and the list moves again from there")
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

// TestClearingASearchFromTheOtherSectionLeavesTheCursor covers escape over a standing search:
// cleared in the section it opened in, the cursor goes back to where the search found it, and
// cleared from the other section, nothing moves.
func TestClearingASearchFromTheOtherSectionLeavesTheCursor(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.agents.cursor = 0

	walked := pressMark(t, m, "/", "a", "u", "t", "enter")
	require.Equal(t, 1, walked.agents.cursor, "the search walked onto the agent")
	require.False(t, walked.isDetailsFocused())

	fromList := send(t, walked, "esc")
	assert.Equal(t, 0, fromList.agents.cursor, "cleared where it opened, the cursor goes back")

	fromRecord := pressMark(t, walked, "ctrl+t", "esc")
	assert.False(t, fromRecord.filter.isStanding())
	assert.Equal(t, 1, fromRecord.agents.cursor, "cleared from the record, the list stays where it is")
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

// TestMatchKeysStayInTheirPane covers n and N walking the pane holding the keys and no further:
// on the list they wrap through the list's matches, and in the record, with the same search
// standing, they wrap through the record's.
func TestMatchKeysStayInTheirPane(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.agents.cursor = 0

	standing := pressMark(t, m, "/", "b", "a", "c", "k", "enter")
	require.Equal(t, 0, standing.agents.cursor, "the header backend is the list's only match")

	same := send(t, standing, "n")
	assert.False(t, same.isDetailsFocused(), "n stays on the list")
	assert.Equal(t, 0, same.agents.cursor, "and wraps to the only match")

	into := send(t, standing, "ctrl+t")
	require.True(t, into.isDetailsFocused())
	held := into.hoveredDetails()
	assert.Equal(t, "name", held.fields[into.detailsCursor(held)].label)

	round := send(t, into, "n")
	assert.True(t, round.isDetailsFocused(), "n in the record stays in the record")
	assert.Equal(t, "name", held.fields[round.detailsCursor(held)].label, "wrapping to its only match")
}

// TestTypingMovesNothingUntilApplied covers the search lighting both panes while it is typed
// and moving nothing, and enter walking only the pane holding the keys: on the list the cursor
// goes to the nearest agent that matches, whatever the record below has lit.
func TestTypingMovesNothingUntilApplied(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	require.Equal(t, 1, m.agents.cursor)

	typed := pressMark(t, m, "/", "b", "a", "c", "k")
	assert.Equal(t, 1, typed.agents.cursor, "the header matches and the cursor stays put")
	assert.False(t, typed.isDetailsFocused())

	entered := send(t, typed, "enter")
	assert.False(t, entered.isDetailsFocused(), "enter never leaves the list")
	assert.Equal(t, 0, entered.agents.cursor, "it walks onto the header")

	missed := pressMark(t, m, "/", "a", "b", "c", "enter")
	assert.False(t, missed.isDetailsFocused())
	assert.Equal(t, 1, missed.agents.cursor, "a pattern only the record matches leaves the list where it is")

	filtered := pressMark(t, typed, "tab", "enter")
	assert.False(t, filtered.isDetailsFocused())
	assert.Len(t, filtered.agents.rows(filtered.shared()), len(m.agents.rows(m.shared())), "a header match keeps the rows under it")
	assert.Len(t, filtered.detailsRows(filtered.hoveredDetails()), len(m.hoveredDetails().fields), "and the record is not narrowed")
}

// TestEscapeAfterTheJumpIntoTheRecord covers escape over a search that ctrl+t carried into the
// record: the search goes, the keys stay on the record where the jump left them, and the list's
// cursor, which never moved, stays.
func TestEscapeAfterTheJumpIntoTheRecord(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	jumped := pressMark(t, m, "/", "a", "b", "c", "ctrl+t")
	require.True(t, jumped.isDetailsFocused())
	held := jumped.hoveredDetails()
	require.Equal(t, "session_id", held.fields[jumped.detailsCursor(held)].label)

	back := send(t, jumped, "esc")
	assert.False(t, back.filter.isStanding())
	assert.True(t, back.isDetailsFocused())
	assert.Equal(t, "session_id", held.fields[back.detailsCursor(held)].label)
	assert.Equal(t, 1, back.agents.cursor)
}

// TestFieldOnlyFilterEmptiesTheList pins the filter narrowing the list on row names alone: a
// pattern that matches a field of the hovered record and no row's name empties the list on
// enter, and the record goes with it.
func TestFieldOnlyFilterEmptiesTheList(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	narrowed := pressMark(t, m, "/", "a", "b", "c", "tab", "enter")

	assert.Empty(t, narrowed.agents.rows(narrowed.shared()))
	assert.False(t, narrowed.isDetailsFocused())
	assert.Contains(t, ansi.Strip(narrowed.View()), "nothing matches")
}

package ui

import (
	"weave/internal/ui/paint"

	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRowCursorBlinksWhileFocused covers the arrow the lists draw: it goes for the hidden half of
// the blink and comes back for the shown one, and it keeps the width either way so the row it
// marks does not slide sideways twice a second.
func TestRowCursorBlinksWhileFocused(t *testing.T) {
	m := model{rowCursor: newRowCursor()}

	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.cursorPrefix(sectionList), "a shown cursor draws the arrow")

	m.rowCursor.Blink = true

	assert.Equal(t, blankCursor, m.cursorPrefix(sectionList))
	assert.Equal(t, lipgloss.Width(rowCursorGlyph), lipgloss.Width(blankCursor), "and leaves the room the arrow took")
}

// TestRowCursorFollowsTheKeys covers the arrow going with the keys: the section holding them
// blinks it, one the keyboard has left for another pane draws it solid, and a section of the
// panel that does not hold them draws nothing where it would stand.
func TestRowCursorFollowsTheKeys(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.rowCursor = newRowCursor()
	m.rowCursor.Blink = true

	assert.Equal(t, blankCursor, m.cursorPrefix(sectionList), "the list holds the keys and is in the hidden half")
	assert.Equal(t, blankCursor, m.cursorPrefix(sectionRecord), "the record draws no arrow while the list has them")

	m.keysElsewhere = true

	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.cursorPrefix(sectionList), "another pane holding them stops the blink")

	m.keysElsewhere = false
	m.details.focused = true
	require.True(t, m.isDetailsFocused())

	assert.Equal(t, blankCursor, m.cursorPrefix(sectionList), "the list draws no arrow while the record has them")
	assert.Equal(t, blankCursor, m.cursorPrefix(sectionRecord), "and the record's is in the hidden half")

	m.rowCursor.Blink = false

	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.cursorPrefix(sectionRecord))
}

// TestRowCursorSolidUnderAKeypress covers moving about: the phase is restarted rather than
// carried, so the row a person just moved to is drawn under an arrow rather than under whichever
// half of the blink the press happened to land in.
func TestRowCursorSolidUnderAKeypress(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.rowCursor = newRowCursor()
	m.rowCursor.Blink = true

	next, cmd := m.Update(keyMsg("j"))
	shown, ok := next.(model)
	require.True(t, ok)

	assert.False(t, shown.rowCursor.Blink)
	assert.NotNil(t, cmd, "and a timer carries the blink on from there")
}

// TestRowCursorFollowsTheKeyboard covers the panel losing and taking back the keyboard, which
// the model records while the bubble's timer runs on through both.
func TestRowCursorFollowsTheKeyboard(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.rowCursor = newRowCursor()

	left, _ := m.Update(tea.BlurMsg{})
	blurred, ok := left.(model)
	require.True(t, ok)
	assert.True(t, blurred.blurred)
	assert.NotNil(t, blurred.rowCursor.BlinkCmd(), "the bubble keeps its timer through a blur")

	back, _ := blurred.Update(tea.FocusMsg{})
	focused, ok := back.(model)
	require.True(t, ok)
	assert.False(t, focused.blurred)
}

// TestRowCursorStartsBlinkingOnItsOwn covers the blink the panel opens with.
func TestRowCursorStartsBlinkingOnItsOwn(t *testing.T) {
	m := model{rowCursor: newRowCursor()}

	_, cmd := m.Update(cursor.Blink())

	assert.NotNil(t, cmd, "the opening blink starts the timer")
}

// TestRowCursorTimerFlipsThePhase runs the command the panel handed back rather than asserting
// it was handed one.
func TestRowCursorTimerFlipsThePhase(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.rowCursor = newRowCursor()
	m.tree.cursor = 0

	next, cmd := m.Update(keyMsg("j"))
	shown, ok := next.(model)
	require.True(t, ok)
	require.False(t, shown.rowCursor.Blink)

	blinked := false
	for _, msg := range drain(cmd) {
		if _, ok := msg.(cursor.BlinkMsg); !ok {
			continue
		}

		after, _ := shown.Update(msg)
		flipped, ok := after.(model)
		require.True(t, ok)
		blinked = flipped.rowCursor.Blink
	}

	assert.True(t, blinked, "the timer's own message is what hides the arrow")
}

// drain runs a command and every command a batch of them holds, handing back the messages they
// sent.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, held := range batch {
			msgs = append(msgs, drain(held)...)
		}

		return msgs
	}

	return []tea.Msg{msg}
}

// TestRowCursorStandsStillWhileTyping covers a search being typed at, on the archive's own filter
// and on the search screen's query, neither of which moves the arrows, so the arrow stands still
// while keeping the colour that says the list is still the thing being driven.
func TestRowCursorStandsStillWhileTyping(t *testing.T) {
	m := model{rowCursor: newRowCursor(), screen: screenArchive}
	m.rowCursor.Blink = true
	m.filter.typing = true

	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.cursorPrefix(sectionList), "the filter has the arrows")

	m.filter.typing = false
	m.screen = screenSearch
	m.search.typing = true

	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.cursorPrefix(sectionList), "the query has them")
}

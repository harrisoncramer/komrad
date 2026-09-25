package ui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRowCursor covers the arrow the lists draw on the hovered row and how it blinks.
func TestRowCursor(t *testing.T) {
	t.Run("the blink hides the arrow and keeps its room", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		shown := harness.readFrame()
		require.Equal(t, "  auth", readHoveredRow(shown))

		harness.fireTimers()

		hidden := harness.readFrame()
		assert.Empty(t, readHoveredRow(hidden))
		assert.Equal(t, findBarColumn(findRowWith(t, shown, "auth")), findBarColumn(findRowWith(t, hidden, "auth")))
	})

	t.Run("a keypress draws the arrow on the row it lands on", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.fireTimers()

		harness.press("j")

		assert.Equal(t, "  api", readHoveredRow(harness.readFrame()))
	})

	t.Run("the timer a keypress starts hides the arrow again", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("j")

		harness.fireTimers()

		assert.Empty(t, readHoveredRow(harness.readFrame()))
	})

	t.Run("another pane holding the keys draws the arrow solid", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.panel.SetKeysElsewhere(true)
		harness.send(tea.BlurMsg{})

		harness.fireTimers()

		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("the keys coming back to the panel start the blink again", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.panel.SetKeysElsewhere(true)
		harness.send(tea.BlurMsg{})
		harness.panel.SetKeysElsewhere(false)

		harness.send(tea.FocusMsg{})
		harness.fireTimers()

		assert.Empty(t, readHoveredRow(harness.readFrame()))
	})

	t.Run("a filter being typed holds the arrow still", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("/", "a")

		harness.fireTimers()

		assert.Equal(t, "  auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("the record holding the keys takes the arrow and blinks it", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.press("ctrl+a")

		shown := harness.readFrame()
		assert.False(t, strings.HasPrefix(findRowWith(t, shown, "auth"), "›"), "the list draws no arrow")
		assert.Equal(t, "name       auth", readRecordCursor(shown))

		harness.fireTimers()

		assert.Empty(t, readRecordCursor(harness.readFrame()))
	})
}

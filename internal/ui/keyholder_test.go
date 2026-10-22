package ui_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeyholder covers which section renders the arrow on its hovered row.
func TestKeyholder(t *testing.T) {
	t.Run("the arrow stays on the hovered row while the timers run", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.fireTimers()

		assert.Equal(t, "├─ auth", readHoveredRow(harness.readFrame()))
	})

	t.Run("a keypress renders the arrow on the row it lands on", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.press("j")

		assert.Equal(t, "├─ api", readHoveredRow(harness.readFrame()))
	})

	t.Run("the record holding the keys takes the arrow", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.press("pick:record")

		shown := harness.readFrame()
		assert.False(t, strings.HasPrefix(findRowWith(t, shown, "auth"), "›"), "the list renders no arrow")
		assert.Equal(t, "agent      —", readRecordCursor(shown))
	})
}

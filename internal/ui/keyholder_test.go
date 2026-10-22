package ui_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"

	"github.com/stretchr/testify/assert"
)

// TestKeyholder covers which section renders the arrow on its hovered row.
func TestKeyholder(t *testing.T) {
	t.Run("the arrow stays on the hovered row while the timers run", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.Advance(time.Second)

		assert.Equal(t, "├─ auth", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("a keypress renders the arrow on the row it lands on", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.Press("j")

		assert.Equal(t, "├─ api", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("the record holding the keys takes the arrow", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.Press("pick:record")

		shown := harness.Frame()
		assert.False(t, strings.HasPrefix(findRowWith(t, shown, "auth"), "›"), "the list renders no arrow")
		assert.Equal(t, "agent      —", paneltest.RecordCursor(shown))
	})
}

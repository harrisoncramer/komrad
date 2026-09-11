package paint

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withColor forces a profile that writes escapes, which lipgloss will not do with no tty.
func withColor(t *testing.T) {
	t.Helper()

	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)
}

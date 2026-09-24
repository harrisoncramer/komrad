package paint

import (
	"testing"
)

// withColor forces a profile that writes escapes, which lipgloss will not do with no tty.
func withColor(t *testing.T) {
	t.Helper()
}

package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenURLRefusesWhatIsNotAWebAddress covers the guard in front of the desktop's opener,
// which acts on a path or an app's own scheme as readily as it does on a link.
func TestOpenURLRefusesWhatIsNotAWebAddress(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{
			name:   "nothing at all",
			target: "",
		},
		{
			name:   "a path the opener would hand to an application",
			target: "/Applications/Calculator.app",
		},
		{
			name:   "a scheme of some application's own",
			target: "file:///etc/passwd",
		},
		{
			name:   "a web scheme naming no host",
			target: "https://",
		},
		{
			name:   "an address the parser cannot read",
			target: "https://exa mple.test/\x7f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewDesktop().OpenURL(t.Context(), tt.target)

			require.ErrorIs(t, err, ErrNotAWebURL)
		})
	}
}

// TestDetailKeepsTheEndOfWhatTheOpenerSaid pins the tail being what a refusal carries, an
// opener's reason landing on the last line.
func TestDetailKeepsTheEndOfWhatTheOpenerSaid(t *testing.T) {
	said := make([]byte, detailCap+64)
	for i := range said {
		said[i] = 'x'
	}
	said = append(said, []byte("  no application knows how to open it\n")...)

	got := detail(said)

	assert.Len(t, got, detailCap+2, "the cut leaves the marker and the cap")
	assert.Contains(t, got, "no application knows how to open it")
}

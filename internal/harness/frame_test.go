package harness_test

import (
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
)

// TestStripDim covers what is left of a captured region once the dim a CLI drew is taken out.
func TestStripDim(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "a region with no dim in it survives whole",
			lines: []string{"❯ half a question about", "  -- INSERT --"},
			want:  []string{"❯ half a question about", "  -- INSERT --"},
		},
		{
			name:  "a dim run that opens and closes on one line goes",
			lines: []string{"❯ \x1b[2mcheck if the author fixed anything\x1b[0m"},
			want:  []string{"❯ "},
		},
		{
			name:  "the same run closed with the attribute reset rather than the whole one",
			lines: []string{"❯ \x1b[2ma suggestion\x1b[22m"},
			want:  []string{"❯ "},
		},
		{
			name:  "a dim run that wrapped goes on the line it wrapped onto",
			lines: []string{"❯ \x1b[2ma suggestion long enough to", "wrap\x1b[0m", "  -- INSERT --"},
			want:  []string{"❯ ", "", "  -- INSERT --"},
		},
		{
			name:  "a reset on the first cell of the next line leaves that line alone",
			lines: []string{"\x1b[2m" + strings.Repeat("─", 4), "\x1b[0m❯ half a question about"},
			want:  []string{"", "❯ half a question about"},
		},
		{
			name:  "text bright before a run that wrapped stays",
			lines: []string{"❯ typed \x1b[2mand suggested", "still suggested\x1b[0m rest"},
			want:  []string{"❯ typed ", " rest"},
		},
		{
			name:  "a region keeps its line count when a line comes down to nothing",
			lines: []string{"\x1b[2mall of it\x1b[0m", "❯ "},
			want:  []string{"", "❯ "},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, harness.StripDim(test.lines))
		})
	}
}

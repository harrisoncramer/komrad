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
			name:  "a run closed by a reset carrying attributes of its own",
			lines: []string{"\x1b[2m" + strings.Repeat("─", 4), "\x1b[0;1m›\x1b[0m \x1b[2ma suggestion\x1b[0m"},
			want:  []string{"", "›\x1b[0m "},
		},
		{
			name:  "a colour whose own component is the reset parameter closes nothing",
			lines: []string{"\x1b[2mdim \x1b[48;5;0mstill dim\x1b[0m bright"},
			want:  []string{" bright"},
		},
		{
			name:  "a colour whose own component is the dim parameter opens nothing",
			lines: []string{"\x1b[38;5;2m• \x1b[1mRan\x1b[0m sleep 25"},
			want:  []string{"\x1b[38;5;2m• \x1b[1mRan\x1b[0m sleep 25"},
		},
		{
			name:  "a region keeps its line count when a line comes down to nothing",
			lines: []string{"\x1b[2mall of it\x1b[0m", "❯ "},
			want:  []string{"", "❯ "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, harness.StripDim(tt.lines))
		})
	}
}

// TestForegrounds covers the colour a line's first character is drawn in.
func TestForegrounds(t *testing.T) {
	const rule = "\x1b[38;5;102m" + "────────"

	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "a marker drawn inside the rule's own colour",
			lines: []string{rule, "❯ \x1b[39m", rule},
			want:  []string{"38;5;102", "38;5;102", "38;5;102"},
		},
		{
			name:  "a marker drawn once the colour has been reset",
			lines: []string{rule, "\x1b[39m❯ ", rule},
			want:  []string{"38;5;102", "", "38;5;102"},
		},
		{
			name:  "a rule closing its own colour leaves the line under it in none",
			lines: []string{rule + "\x1b[39m", "❯ \x1b[2ma suggestion\x1b[0m"},
			want:  []string{"38;5;102", ""},
		},
		{
			name:  "a capture taken without the colours",
			lines: []string{"────────", "❯ "},
			want:  []string{"", ""},
		},
		{
			name:  "a colour named without the extended selector",
			lines: []string{"\x1b[90m────────", "❯ "},
			want:  []string{"90", "90"},
		},
		{
			name:  "the colour is taken from the first run that is not blank",
			lines: []string{"  \x1b[38;5;102m-- INSERT --\x1b[39m"},
			want:  []string{"38;5;102"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, harness.Foregrounds(tt.lines))
		})
	}
}

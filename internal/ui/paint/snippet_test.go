package paint

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// TestClipToMatch pins the match surviving the cut and the row still fitting, wherever the
// match sits in the snippet.
func TestClipToMatch(t *testing.T) {
	const (
		lead  = "the quick brown fox jumps over the lazy dog and keeps on running for a while "
		trail = " and then carries on well past where any pane could show it all"
	)

	marked := func(match string) string {
		return MarkOpen + match + MarkClose
	}

	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{
			name:  "an unmeasured pane clips nothing",
			in:    lead + marked("needle") + trail,
			width: 0,
			want:  "needle",
		},
		{
			name:  "a snippet already narrow enough is left alone",
			in:    "short " + marked("needle"),
			width: 40,
			want:  "needle",
		},
		{
			name:  "a match past the width is pulled into view",
			in:    lead + marked("needle") + trail,
			width: 40,
			want:  "needle",
		},
		{
			name:  "a match at the very end is still shown",
			in:    lead + trail + marked("needle"),
			width: 40,
			want:  "needle",
		},
		{
			name:  "an unmarked snippet is clipped from the start",
			in:    lead + trail,
			width: 40,
			want:  "the quick",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := clipToMatch(tt.in, tt.width)
			assert.Contains(t, out, tt.want, "the match survives the clip")

			if tt.width > 0 {
				assert.LessOrEqual(t, lipgloss.Width(Colored.RenderSnippet(tt.in, tt.width)), tt.width,
					"the rendered row fits the pane")
			}

			opens := strings.Count(out, MarkOpen)
			assert.Equal(t, opens, strings.Count(out, MarkClose),
				"every marker left in the snippet is paired")
		})
	}
}

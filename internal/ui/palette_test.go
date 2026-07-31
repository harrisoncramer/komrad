package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStageStyleRendersEveryElement covers the markdown the stage renders through the
// palette's own style. glamour hands the colours under CodeBlock.Chroma to chroma, which
// reads hex alone and panics on anything else, so a fenced block is the case worth
// holding onto: it took the whole panel down rather than falling back to the source text
// the way a render error does.
func TestStageStyleRendersEveryElement(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
	}{
		{
			name:     "a fenced code block, which reaches chroma",
			markdown: "```go\nfunc main() {}\n```\n",
		},
		{
			name:     "a fenced block in no language at all",
			markdown: "```\nplain\n```\n",
		},
		{
			name:     "an indented block, which chroma also lexes",
			markdown: "    indented\n",
		},
		{
			name:     "the elements a workflow's notes are written in",
			markdown: "# Heading\n\nProse with `code`, *emph*, **strong**, and a [link](http://example.com).\n\n- item\n\n> quoted\n\n---\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer, err := glamour.NewTermRenderer(glamour.WithStyles(stageStyle), glamour.WithWordWrap(60))
			require.NoError(t, err)

			got, err := renderer.Render(tt.markdown)
			require.NoError(t, err)
			assert.NotEmpty(t, strings.TrimSpace(got))
		})
	}
}

// TestPaletteFollowsTheTerminal covers the whole point of the palette: every colour it
// names has to come out as one of the sixteen a terminal's scheme maps, which is the
// escape naming a colour by index. A hex value writes the channels instead, so it would
// look the same on every machine and take nothing from the scheme.
func TestPaletteFollowsTheTerminal(t *testing.T) {
	// The profile is forced because the tests run with no terminal on the other end,
	// where lipgloss writes no escapes at all and every case below would pass on an
	// empty string. TrueColor is the profile that would happily write hex, so it is the
	// one that shows an index staying an index.
	//
	// It is put back afterwards because the profile is process-wide, and the cases
	// elsewhere that read a rendered row count on lipgloss writing nothing.
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	tests := []struct {
		name  string
		color lipgloss.Color
		want  string
	}{
		{
			name:  "accent is the scheme's blue",
			color: colorAccent,
			want:  "\x1b[34m",
		},
		{
			name:  "success is its bright green",
			color: colorSuccess,
			want:  "\x1b[92m",
		},
		{
			name:  "warning is its bright yellow",
			color: colorWarning,
			want:  "\x1b[93m",
		},
		{
			name:  "danger is its bright red",
			color: colorDanger,
			want:  "\x1b[91m",
		},
		{
			name:  "muted is its bright black",
			color: colorMuted,
			want:  "\x1b[90m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lipgloss.NewStyle().Foreground(tt.color).Render("x")
			assert.True(t, strings.HasPrefix(got, tt.want), "want prefix %q, got %q", tt.want, got)
		})
	}
}

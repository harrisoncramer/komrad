package paint

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStageStyleRendersEveryElement pins the fenced code block, whose colours chroma reads as
// hex alone and panics on anything else.
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
			renderer, err := glamour.NewTermRenderer(glamour.WithStyles(Panel.Stage()), glamour.WithWordWrap(60))
			require.NoError(t, err)

			got, err := renderer.Render(tt.markdown)
			require.NoError(t, err)
			assert.NotEmpty(t, strings.TrimSpace(got))
		})
	}
}

// TestPaletteFollowsTheTerminal pins every colour coming out as one of the sixteen the
// terminal's scheme maps.
func TestPaletteFollowsTheTerminal(t *testing.T) {
	tests := []struct {
		name string
		tone Tone
		want string
	}{
		{
			name: "accent is the scheme's blue",
			tone: ToneAccent,
			want: "\x1b[34m",
		},
		{
			name: "success is its bright green",
			tone: ToneSuccess,
			want: "\x1b[92m",
		},
		{
			name: "warning is its bright yellow",
			tone: ToneWarning,
			want: "\x1b[93m",
		},
		{
			name: "danger is its bright red",
			tone: ToneDanger,
			want: "\x1b[91m",
		},
		{
			name: "muted is its bright black",
			tone: ToneMuted,
			want: "\x1b[90m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Panel.Style(tt.tone).Render("x")
			assert.True(t, strings.HasPrefix(got, tt.want), "want prefix %q, got %q", tt.want, got)
		})
	}
}

// TestEveryToneIsItsOwnColour pins that the panel's tones stay told apart.
func TestEveryToneIsItsOwnColour(t *testing.T) {
	seen := map[color.Color]Tone{}

	for _, tone := range []Tone{ToneMuted, ToneAccent, ToneSuccess, ToneWarning, ToneDanger, ToneMark, ToneSelf, ToneOther, ToneBright, ToneMerged} {
		color := Panel.Color(tone)
		other, taken := seen[color]
		assert.False(t, taken, "tone %d draws in the same colour as tone %d", tone, other)
		seen[color] = tone
	}
}

// TestStageCarriesTheTones covers the markdown a block beside a list is drawn in.
func TestStageCarriesTheTones(t *testing.T) {
	stage := Panel.Stage()

	require.NotNil(t, stage.H1.Color)
	require.NotNil(t, stage.Item.Color)
	assert.Equal(t, string(accent), *stage.H1.Color)
	assert.Equal(t, string(success), *stage.Item.Color)
}

// TestMarkTakesItsToneFromThePalette covers the shape every optional mark on a row is drawn in.
func TestMarkTakesItsToneFromThePalette(t *testing.T) {
	assert.Empty(t, Panel.Mark(false, "✓", ToneSuccess), "a fact that is not true draws nothing")
	assert.Equal(t, " "+Panel.Style(ToneSuccess).Render("✓"), Panel.Mark(true, "✓", ToneSuccess))
}

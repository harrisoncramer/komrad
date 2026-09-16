package paint

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
			renderer, err := glamour.NewTermRenderer(glamour.WithStyles(Colored.Stage()), glamour.WithWordWrap(60))
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
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

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
			got := Colored.Style(tt.tone).Render("x")
			assert.True(t, strings.HasPrefix(got, tt.want), "want prefix %q, got %q", tt.want, got)
		})
	}
}

// TestGreyCollapsesEveryTone pins the whole point of the grey palette: a region that does not
// hold the keys carries no hue at all, whatever part each piece of it plays.
func TestGreyCollapsesEveryTone(t *testing.T) {
	tones := []Tone{ToneMuted, ToneAccent, ToneSuccess, ToneWarning, ToneDanger, ToneMark, ToneSelf, ToneOther}

	for _, tone := range tones {
		assert.Equal(t, Grey.Color(ToneMuted), Grey.Color(tone), "tone %d is grey like the rest", tone)
	}

	assert.NotEqual(t, Colored.Color(ToneMuted), Colored.Color(ToneAccent), "the coloured palette still tells them apart")
	assert.NotEqual(t, Colored.Color(ToneSuccess), Colored.Color(ToneDanger))
}

// TestPaletteForPicksTheRegionsOwn covers the one call every region makes.
func TestPaletteForPicksTheRegionsOwn(t *testing.T) {
	assert.Equal(t, Colored, PaletteFor(true))
	assert.Equal(t, Grey, PaletteFor(false))
}

// TestGreyStageCarriesNoHue covers the markdown a block beside a list is drawn in, which greys
// with the block rather than keeping its own colours under it.
func TestGreyStageCarriesNoHue(t *testing.T) {
	grey := Grey.Stage()

	require.NotNil(t, grey.H1.Color)
	require.NotNil(t, grey.Item.Color)
	assert.Equal(t, *grey.H1.Color, *grey.Item.Color, "a heading and a bullet are the same grey")
	assert.Equal(t, string(muted), *grey.H1.Color)

	colored := Colored.Stage()
	require.NotNil(t, colored.H1.Color)
	assert.NotEqual(t, *colored.H1.Color, *grey.H1.Color)
}

// TestMarkTakesItsToneFromThePalette covers the shape every optional mark on a row is drawn in.
func TestMarkTakesItsToneFromThePalette(t *testing.T) {
	withColor(t)

	assert.Empty(t, Colored.Mark(false, "✓", ToneSuccess), "a fact that is not true draws nothing")
	assert.Equal(t, " "+Colored.Style(ToneSuccess).Render("✓"), Colored.Mark(true, "✓", ToneSuccess))
	assert.Equal(t, " "+Grey.Style(ToneMuted).Render("✓"), Grey.Mark(true, "✓", ToneSuccess),
		"the same mark is grey in a region that does not hold the keys")
}

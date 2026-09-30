package paint

import (
	"fmt"
	"image/color"

	"komrad/internal/glyph"
	"komrad/internal/theme"
	"komrad/lib/xptr"

	"charm.land/bubbles/v2/help"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Panel is the palette the panel draws in.
var Panel = defaultPalette()

// Tone is the part a piece of the panel plays, which the palette turns into a colour.
type Tone int

const (
	ToneMuted Tone = iota
	TonePrimary
	ToneSecondary
	ToneSuccess
	ToneWarning
	ToneDanger
	ToneUserMe
	ToneUserAgent
	ToneUserReviewer
	ToneUserGithub
	ToneMerged
	ToneHighlight
	ToneInverse
)

// Shade is the part a background plays, which the palette turns into a colour.
type Shade int

const (
	ShadeDiffAdded Shade = iota
	ShadeDiffDeleted
	ShadeSelection
	ShadeSelectionDim
	ShadeMatch
)

// Palette is the colours the panel draws in.
type Palette struct {
	colors theme.Colors

	// blinkedOut is the off beat of the blink whatever holds the keys is drawn on.
	blinkedOut bool

	// stage is held behind a pointer.
	stage *ansi.StyleConfig
}

// NewPalette is the palette drawing in the given colours, refusing one it cannot draw.
func NewPalette(colors theme.Colors) (*Palette, error) {
	if err := theme.Check(colors); err != nil {
		return nil, fmt.Errorf("failed to build the palette: %w", err)
	}

	palette := Palette{colors: colors}
	style := stageStyle(palette)
	palette.stage = &style

	return &palette, nil
}

// Use makes palette the one the panel draws in.
func Use(palette Palette) {
	Panel = palette
}

// Color is what a tone looks like in this palette.
func (p Palette) Color(tone Tone) color.Color {
	return lipgloss.Color(p.spell(tone))
}

// ANSIColor is a tone in the form glamour's style config wants it.
func (p Palette) ANSIColor(tone Tone) *string {
	return xptr.Ptr(p.spell(tone))
}

// Background is what a shade looks like in this palette on the terminal's own background.
func (p Palette) Background(shade Shade) Adaptive {
	pair := p.pair(shade)

	return Adaptive{Light: lipgloss.Color(pair.Light), Dark: lipgloss.Color(pair.Dark)}
}

// Style is a foreground of one tone and nothing else.
func (p Palette) Style(tone Tone) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.Color(tone))
}

// FormTheme is the huh theme a form is drawn in.
func (p Palette) FormTheme() huh.Theme {
	return huh.ThemeFunc(p.formStyles)
}

// HelpStyles is the legend's styling, muted throughout.
func (p Palette) HelpStyles() help.Styles {
	dim := p.Style(ToneMuted)

	return help.Styles{
		ShortKey:       dim,
		ShortDesc:      dim,
		ShortSeparator: dim,
		Ellipsis:       dim,
		FullKey:        dim,
		FullDesc:       dim,
		FullSeparator:  dim,
	}
}

// Stage is the markdown a block beside a list is drawn in.
func (p Palette) Stage() ansi.StyleConfig {
	return *p.stage
}

func (p Palette) spell(tone Tone) string {
	switch tone {
	case TonePrimary:
		return p.colors.Primary
	case ToneSecondary:
		return p.colors.Secondary
	case ToneSuccess:
		return p.colors.Success
	case ToneWarning:
		return p.colors.Warning
	case ToneDanger:
		return p.colors.Danger
	case ToneMuted:
		return p.colors.Muted
	case ToneUserMe:
		return p.colors.UserMe
	case ToneUserAgent:
		return p.colors.UserAgent
	case ToneUserReviewer:
		return p.colors.UserReviewer
	case ToneUserGithub:
		return p.colors.UserGithub
	case ToneMerged:
		return p.colors.Merged
	case ToneHighlight:
		return p.colors.Highlight
	case ToneInverse:
		return p.colors.Inverse
	}

	return p.colors.Muted
}

func (p Palette) pair(shade Shade) theme.Pair {
	switch shade {
	case ShadeDiffAdded:
		return p.colors.DiffAdded
	case ShadeDiffDeleted:
		return p.colors.DiffDeleted
	case ShadeSelection:
		return p.colors.Selection
	case ShadeSelectionDim:
		return p.colors.SelectionDim
	case ShadeMatch:
		return p.colors.Match
	}

	return p.colors.Selection
}

func (p Palette) formStyles(isDark bool) *huh.Styles {
	theme := huh.ThemeBase(isDark)
	primary, muted, danger := p.Color(TonePrimary), p.Color(ToneMuted), p.Color(ToneDanger)

	theme.Focused.Base = theme.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Focused.Card = theme.Focused.Base
	theme.Focused.Title = theme.Focused.Title.Foreground(primary).Bold(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(primary).Bold(true)
	theme.Focused.Description = theme.Focused.Description.Foreground(muted)
	theme.Focused.Directory = theme.Focused.Directory.Foreground(primary)
	theme.Focused.ErrorIndicator = theme.Focused.ErrorIndicator.Foreground(danger)
	theme.Focused.ErrorMessage = theme.Focused.ErrorMessage.Foreground(danger)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(primary).SetString(glyph.Focus + " ")
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(primary)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(primary)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(primary)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(primary)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(primary)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(p.Color(ToneInverse)).Background(primary)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(primary)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(primary)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(muted)

	theme.Blurred = theme.Focused
	theme.Blurred.Title = theme.Blurred.Title.Foreground(muted).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(muted).Bold(false)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(muted)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()

	theme.Help = p.HelpStyles()

	return theme
}

// defaultPalette is the palette drawing in the default colours.
func defaultPalette() Palette {
	palette, err := NewPalette(theme.Defaults())
	if err != nil {
		panic(fmt.Sprintf("the default colours do not draw: %v", err))
	}

	return *palette
}

// tmuxColor is a colour spelled the way tmux's style markup wants it.
func tmuxColor(spelled string) string {
	if theme.IsNumber(spelled) {
		return "colour" + spelled
	}

	return spelled
}

// stageStyle is the markdown styling of one palette.
func stageStyle(palette Palette) ansi.StyleConfig {
	style := styles.DarkStyleConfig

	style.Document.Color = nil
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Document.Margin = nil
	style.Text.Color = nil
	style.Heading.Color = palette.ANSIColor(TonePrimary)
	style.H1.Color = palette.ANSIColor(TonePrimary)
	style.H1.BackgroundColor = nil
	style.H2.Color = palette.ANSIColor(TonePrimary)
	style.H3.Color = palette.ANSIColor(TonePrimary)
	style.H4.Color = palette.ANSIColor(TonePrimary)
	style.H5.Color = palette.ANSIColor(TonePrimary)
	style.H6.Color = palette.ANSIColor(TonePrimary)
	style.BlockQuote.Color = palette.ANSIColor(ToneMuted)
	style.Item.Color = palette.ANSIColor(ToneSecondary)
	style.Enumeration.Color = palette.ANSIColor(ToneSecondary)

	style.Enumeration.BlockPrefix = ") "
	style.Task.Color = palette.ANSIColor(ToneSecondary)
	style.Emph.Color = palette.ANSIColor(ToneSecondary)
	style.Strong.Color = palette.ANSIColor(ToneSecondary)
	style.HorizontalRule.Color = palette.ANSIColor(ToneMuted)
	style.Link.Color = palette.ANSIColor(ToneMuted)
	style.LinkText.Color = palette.ANSIColor(TonePrimary)
	style.Image.Color = palette.ANSIColor(ToneMuted)
	style.ImageText.Color = palette.ANSIColor(TonePrimary)
	style.Code.Color = palette.ANSIColor(ToneSecondary)
	style.Code.BackgroundColor = nil
	style.CodeBlock.Color = nil

	style.CodeBlock.Chroma = nil
	style.Table.Color = nil
	style.DefinitionTerm.Color = palette.ANSIColor(TonePrimary)
	style.DefinitionDescription.Color = nil
	style.HTMLBlock.Color = palette.ANSIColor(ToneMuted)
	style.HTMLSpan.Color = palette.ANSIColor(ToneMuted)

	return style
}

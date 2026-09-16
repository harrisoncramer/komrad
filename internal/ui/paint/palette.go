package paint

import (
	"weave/lib/xptr"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// The panel's whole palette, as terminal colour numbers so a theme the user has set decides
// what each one looks like.
const (
	accent  = lipgloss.Color("4")
	success = lipgloss.Color("10")
	warning = lipgloss.Color("11")
	danger  = lipgloss.Color("9")
	muted   = lipgloss.Color("8")
	bright  = lipgloss.Color("12")
	alt     = lipgloss.Color("13")
	// markColor is the plain yellow rather than the bright one a thinking agent is drawn in, so
	// a mark in the gutter cannot be read as a lifecycle state.
	markColor = lipgloss.Color("3")
)

// Tone is the part a piece of the panel plays, which the palette of the region drawing it turns
// into a colour.
type Tone int

const (
	ToneMuted Tone = iota
	ToneAccent
	ToneSuccess
	ToneWarning
	ToneDanger
	ToneMark
	// ToneBright is the accent's brighter twin, and ToneAlt the one colour nothing else uses,
	// for the places two things have to be told apart at a glance.
	ToneBright
	ToneAlt
)

// Palette is what one region of the panel draws in, and there are two of them: the colours for
// the region holding the keys, and grey for every other.
type Palette struct {
	accent    lipgloss.Color
	success   lipgloss.Color
	warning   lipgloss.Color
	danger    lipgloss.Color
	muted     lipgloss.Color
	mark      lipgloss.Color
	bright    lipgloss.Color
	alt       lipgloss.Color
	selection lipgloss.AdaptiveColor
	stage     ansi.StyleConfig
}

// Colored is what the region holding the keys draws in.
var Colored = withStage(Palette{
	accent:    accent,
	success:   success,
	warning:   warning,
	danger:    danger,
	muted:     muted,
	mark:      markColor,
	bright:    bright,
	alt:       alt,
	selection: lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"},
})

// Grey is what every region that does not hold the keys draws in, one colour throughout so the
// eye is left with nowhere but the active region to land.
var Grey = withStage(Palette{
	accent:    muted,
	success:   muted,
	warning:   muted,
	danger:    muted,
	muted:     muted,
	mark:      muted,
	bright:    muted,
	alt:       muted,
	selection: lipgloss.AdaptiveColor{Light: "#e5e5e5", Dark: "#303030"},
})

// withStage fills in the markdown styling a palette's own colours make.
func withStage(palette Palette) Palette {
	palette.stage = stageStyle(palette)

	return palette
}

// PaletteFor is the palette a region draws in, given whether it holds the keys.
func PaletteFor(focused bool) Palette {
	if focused {
		return Colored
	}

	return Grey
}

// Color is what a tone looks like in this palette.
func (p Palette) Color(tone Tone) lipgloss.Color {
	switch tone {
	case ToneAccent:
		return p.accent
	case ToneSuccess:
		return p.success
	case ToneWarning:
		return p.warning
	case ToneDanger:
		return p.danger
	case ToneMuted:
		return p.muted
	case ToneMark:
		return p.mark
	case ToneBright:
		return p.bright
	case ToneAlt:
		return p.alt
	}

	return p.muted
}

// Style is a foreground of one tone and nothing else.
func (p Palette) Style(tone Tone) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.Color(tone))
}

// FormTheme is the huh theme a form is drawn in.
func (p Palette) FormTheme() *huh.Theme {
	theme := huh.ThemeBase()

	theme.Focused.Base = theme.Focused.Base.BorderForeground(p.muted)
	theme.Focused.Title = theme.Focused.Title.Foreground(p.accent).Bold(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(p.accent).Bold(true)
	theme.Focused.Description = theme.Focused.Description.Foreground(p.muted)
	theme.Focused.Directory = theme.Focused.Directory.Foreground(p.accent)
	theme.Focused.ErrorIndicator = theme.Focused.ErrorIndicator.Foreground(p.danger)
	theme.Focused.ErrorMessage = theme.Focused.ErrorMessage.Foreground(p.danger)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(p.warning)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(p.warning)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(p.success)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(p.success)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(p.accent)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(p.accent)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(p.accent)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(p.warning)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(p.accent)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(p.muted)

	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base
	theme.Blurred.Title = theme.Blurred.Title.Foreground(p.muted).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(p.muted).Bold(false)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(p.muted)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()

	theme.Help = HelpStyles()

	return theme
}

// HelpStyles is the legend's styling, muted throughout.
func HelpStyles() help.Styles {
	dim := lipgloss.NewStyle().Foreground(muted)

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
	return p.stage
}

// stageStyle is the markdown styling of one palette.
func stageStyle(palette Palette) ansi.StyleConfig {
	style := styles.DarkStyleConfig

	style.Document.Color = nil
	style.Text.Color = nil
	style.Heading.Color = ANSIColor(palette.accent)
	style.H1.Color = ANSIColor(palette.accent)
	style.H1.BackgroundColor = nil
	style.H2.Color = ANSIColor(palette.accent)
	style.H3.Color = ANSIColor(palette.accent)
	style.H4.Color = ANSIColor(palette.accent)
	style.H5.Color = ANSIColor(palette.accent)
	style.H6.Color = ANSIColor(palette.accent)
	style.BlockQuote.Color = ANSIColor(palette.muted)
	style.Item.Color = ANSIColor(palette.success)
	style.Enumeration.Color = ANSIColor(palette.success)

	style.Enumeration.BlockPrefix = ") "
	style.Task.Color = ANSIColor(palette.success)
	style.Emph.Color = ANSIColor(palette.warning)
	style.Strong.Color = ANSIColor(palette.warning)
	style.HorizontalRule.Color = ANSIColor(palette.muted)
	style.Link.Color = ANSIColor(palette.accent)
	style.LinkText.Color = ANSIColor(palette.warning)
	style.Image.Color = ANSIColor(palette.accent)
	style.ImageText.Color = ANSIColor(palette.muted)
	style.Code.Color = ANSIColor(palette.warning)
	style.Code.BackgroundColor = nil
	style.CodeBlock.Color = nil

	style.CodeBlock.Chroma = nil
	style.Table.Color = nil
	style.DefinitionTerm.Color = ANSIColor(palette.accent)
	style.DefinitionDescription.Color = nil
	style.HTMLBlock.Color = ANSIColor(palette.muted)
	style.HTMLSpan.Color = ANSIColor(palette.muted)

	return style
}

// ANSIColor is a colour in the form glamour's style config wants it.
func ANSIColor(color lipgloss.Color) *string {
	return xptr.Ptr(string(color))
}

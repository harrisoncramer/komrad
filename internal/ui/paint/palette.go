package paint

import (
	"image/color"

	"komrad/lib/xptr"

	"charm.land/bubbles/v2/help"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// colorNumber is a terminal colour number, so a theme the user has set decides what it looks like.
type colorNumber string

// The panel's whole palette.
const (
	accent       colorNumber = "4"
	success      colorNumber = "10"
	warning      colorNumber = "11"
	danger       colorNumber = "9"
	muted        colorNumber = "8"
	speakerSelf  colorNumber = "12"
	speakerOther colorNumber = "13"
	// bright is a foreground above the terminal's own default.
	bright colorNumber = "15"
	// merged is the plain magenta, kept off the bright one the other voice in a conversation
	// wears so retuning the speakers leaves a landed change alone.
	merged colorNumber = "5"
	// markColor is the plain yellow rather than the bright one a thinking agent is drawn in, so
	// a mark in the gutter cannot be read as a lifecycle state.
	markColor colorNumber = "3"
)

// Black is the terminal's own black, drawn on a coloured bar.
var Black = lipgloss.Color("0")

func (c colorNumber) color() color.Color {
	return lipgloss.Color(string(c))
}

// Tone is the part a piece of the panel plays, which the palette turns into a colour.
type Tone int

const (
	ToneMuted Tone = iota
	ToneAccent
	ToneSuccess
	ToneWarning
	ToneDanger
	ToneMark
	// ToneSelf and ToneOther are the two voices in a conversation, told apart at a glance.
	ToneSelf
	ToneOther
	ToneBright
	ToneMerged
)

// Palette is the colours the panel draws in.
type Palette struct {
	accent  colorNumber
	success colorNumber
	warning colorNumber
	danger  colorNumber
	muted   colorNumber
	mark    colorNumber
	self    colorNumber
	other   colorNumber
	bright  colorNumber
	merged  colorNumber

	// stage is held behind a pointer.
	stage *ansi.StyleConfig
}

// Panel is the palette the panel draws in.
var Panel = withStage(Palette{
	accent:  accent,
	success: success,
	warning: warning,
	danger:  danger,
	muted:   muted,
	mark:    markColor,
	self:    speakerSelf,
	other:   speakerOther,
	bright:  bright,
	merged:  merged,
})

// withStage fills in the markdown styling a palette's own colours make.
func withStage(palette Palette) Palette {
	style := stageStyle(palette)
	palette.stage = &style

	return palette
}

// Color is what a tone looks like in this palette.
func (p Palette) Color(tone Tone) color.Color {
	return p.number(tone).color()
}

// ANSIColor is a tone in the form glamour's style config wants it.
func (p Palette) ANSIColor(tone Tone) *string {
	return ANSIColor(p.number(tone))
}

func (p Palette) number(tone Tone) colorNumber {
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
	case ToneSelf:
		return p.self
	case ToneOther:
		return p.other
	case ToneBright:
		return p.bright
	case ToneMerged:
		return p.merged
	}

	return p.muted
}

// Style is a foreground of one tone and nothing else.
func (p Palette) Style(tone Tone) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.Color(tone))
}

// FormTheme is the huh theme a form is drawn in.
func (p Palette) FormTheme() huh.Theme {
	return huh.ThemeFunc(p.formStyles)
}

func (p Palette) formStyles(isDark bool) *huh.Styles {
	theme := huh.ThemeBase(isDark)

	theme.Focused.Base = theme.Focused.Base.BorderForeground(p.muted.color())
	theme.Focused.Title = theme.Focused.Title.Foreground(p.accent.color()).Bold(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(p.accent.color()).Bold(true)
	theme.Focused.Description = theme.Focused.Description.Foreground(p.muted.color())
	theme.Focused.Directory = theme.Focused.Directory.Foreground(p.accent.color())
	theme.Focused.ErrorIndicator = theme.Focused.ErrorIndicator.Foreground(p.danger.color())
	theme.Focused.ErrorMessage = theme.Focused.ErrorMessage.Foreground(p.danger.color())
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(p.warning.color())
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(p.warning.color())
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(p.accent.color())
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(p.accent.color())
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(p.accent.color())
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(p.accent.color())
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(Black).Background(p.accent.color())
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(p.warning.color())
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(p.accent.color())
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(p.muted.color())

	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base
	theme.Blurred.Title = theme.Blurred.Title.Foreground(p.muted.color()).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(p.muted.color()).Bold(false)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(p.muted.color())
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()

	theme.Help = HelpStyles()

	return theme
}

// HelpStyles is the legend's styling, muted throughout.
func HelpStyles() help.Styles {
	dim := lipgloss.NewStyle().Foreground(muted.color())

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

// stageStyle is the markdown styling of one palette.
func stageStyle(palette Palette) ansi.StyleConfig {
	style := styles.DarkStyleConfig

	style.Document.Color = nil
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Document.Margin = nil
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

// ANSIColor is a colour number in the form glamour's style config wants it.
func ANSIColor(number colorNumber) *string {
	return xptr.Ptr(string(number))
}

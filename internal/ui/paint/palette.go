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
	Accent  = lipgloss.Color("4")
	Success = lipgloss.Color("10")
	Warning = lipgloss.Color("11")
	Danger  = lipgloss.Color("9")
	Muted   = lipgloss.Color("8")
	// Mark is the plain yellow rather than the bright one a thinking agent is drawn in, so
	// a mark in the gutter cannot be read as a lifecycle state.
	Mark = lipgloss.Color("3")
)

// FormTheme is the huh theme every form in the panel is drawn in.
func FormTheme() *huh.Theme {
	theme := huh.ThemeBase()

	theme.Focused.Base = theme.Focused.Base.BorderForeground(Muted)
	theme.Focused.Title = theme.Focused.Title.Foreground(Accent).Bold(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(Accent).Bold(true)
	theme.Focused.Description = theme.Focused.Description.Foreground(Muted)
	theme.Focused.Directory = theme.Focused.Directory.Foreground(Accent)
	theme.Focused.ErrorIndicator = theme.Focused.ErrorIndicator.Foreground(Danger)
	theme.Focused.ErrorMessage = theme.Focused.ErrorMessage.Foreground(Danger)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(Warning)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(Warning)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(Success)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(Success)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(Accent)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(Accent)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(Accent)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(Warning)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(Accent)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(Muted)

	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base
	theme.Blurred.Title = theme.Blurred.Title.Foreground(Muted).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(Muted).Bold(false)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(Muted)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()

	theme.Help = HelpStyles()

	return theme
}

// HelpStyles is the legend's styling, muted so the keys never compete with the list above them.
func HelpStyles() help.Styles {
	muted := lipgloss.NewStyle().Foreground(Muted)

	return help.Styles{
		ShortKey:       muted,
		ShortDesc:      muted,
		ShortSeparator: muted,
		Ellipsis:       muted,
		FullKey:        muted,
		FullDesc:       muted,
		FullSeparator:  muted,
	}
}

// StageStyle is the markdown every block beside a list is drawn in.
var StageStyle = func() ansi.StyleConfig {
	style := styles.DarkStyleConfig

	style.Document.Color = nil
	style.Text.Color = nil
	style.Heading.Color = ANSIColor(Accent)
	style.H1.Color = ANSIColor(Accent)
	style.H1.BackgroundColor = nil
	style.H2.Color = ANSIColor(Accent)
	style.H3.Color = ANSIColor(Accent)
	style.H4.Color = ANSIColor(Accent)
	style.H5.Color = ANSIColor(Accent)
	style.H6.Color = ANSIColor(Accent)
	style.BlockQuote.Color = ANSIColor(Muted)
	style.Item.Color = ANSIColor(Success)
	style.Enumeration.Color = ANSIColor(Success)

	style.Enumeration.BlockPrefix = ") "
	style.Task.Color = ANSIColor(Success)
	style.Emph.Color = ANSIColor(Warning)
	style.Strong.Color = ANSIColor(Warning)
	style.HorizontalRule.Color = ANSIColor(Muted)
	style.Link.Color = ANSIColor(Accent)
	style.LinkText.Color = ANSIColor(Warning)
	style.Image.Color = ANSIColor(Accent)
	style.ImageText.Color = ANSIColor(Muted)
	style.Code.Color = ANSIColor(Warning)
	style.Code.BackgroundColor = nil
	style.CodeBlock.Color = nil

	style.CodeBlock.Chroma = nil
	style.Table.Color = nil
	style.DefinitionTerm.Color = ANSIColor(Accent)
	style.DefinitionDescription.Color = nil
	style.HTMLBlock.Color = ANSIColor(Muted)
	style.HTMLSpan.Color = ANSIColor(Muted)

	return style
}()

// ANSIColor is a colour in the form glamour's style config wants it.
func ANSIColor(color lipgloss.Color) *string {
	return xptr.Ptr(string(color))
}

// BlurredStageStyle is the markdown of a block beside the list while the keys are somewhere
// else, drawn as one grey column rather than competing with the half that holds them.
var BlurredStageStyle = func() ansi.StyleConfig {
	style := StageStyle
	muted := ANSIColor(Muted)

	style.Heading.Color = muted
	style.H1.Color = muted
	style.H2.Color = muted
	style.H3.Color = muted
	style.H4.Color = muted
	style.H5.Color = muted
	style.H6.Color = muted
	style.Item.Color = muted
	style.Enumeration.Color = muted
	style.Task.Color = muted
	style.Emph.Color = muted
	style.Strong.Color = muted
	style.Link.Color = muted
	style.LinkText.Color = muted
	style.Image.Color = muted
	style.ImageText.Color = muted
	style.Code.Color = muted
	style.DefinitionTerm.Color = muted

	return style
}()

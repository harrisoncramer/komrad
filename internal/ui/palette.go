package ui

import (
	"weave/lib/xptr"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const (
	colorAccent  = lipgloss.Color("4")
	colorSuccess = lipgloss.Color("10")
	colorWarning = lipgloss.Color("11")
	colorDanger  = lipgloss.Color("9")
	colorMuted   = lipgloss.Color("8")
)

func formTheme() *huh.Theme {
	theme := huh.ThemeBase()

	theme.Focused.Base = theme.Focused.Base.BorderForeground(colorMuted)
	theme.Focused.Title = theme.Focused.Title.Foreground(colorAccent).Bold(true)
	theme.Focused.NoteTitle = theme.Focused.NoteTitle.Foreground(colorAccent).Bold(true)
	theme.Focused.Description = theme.Focused.Description.Foreground(colorMuted)
	theme.Focused.Directory = theme.Focused.Directory.Foreground(colorAccent)
	theme.Focused.ErrorIndicator = theme.Focused.ErrorIndicator.Foreground(colorDanger)
	theme.Focused.ErrorMessage = theme.Focused.ErrorMessage.Foreground(colorDanger)
	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(colorWarning)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(colorWarning)
	theme.Focused.SelectedOption = theme.Focused.SelectedOption.Foreground(colorSuccess)
	theme.Focused.SelectedPrefix = theme.Focused.SelectedPrefix.Foreground(colorSuccess)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(colorAccent)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(colorAccent)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(lipgloss.Color("0")).Background(colorAccent)
	theme.Focused.TextInput.Cursor = theme.Focused.TextInput.Cursor.Foreground(colorWarning)
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(colorAccent)
	theme.Focused.TextInput.Placeholder = theme.Focused.TextInput.Placeholder.Foreground(colorMuted)

	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base
	theme.Blurred.Title = theme.Blurred.Title.Foreground(colorMuted).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(colorMuted).Bold(false)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(colorMuted)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()

	theme.Help = helpStyles()

	return theme
}

func helpStyles() help.Styles {
	muted := lipgloss.NewStyle().Foreground(colorMuted)

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

var stageStyle = func() ansi.StyleConfig {
	style := styles.DarkStyleConfig

	style.Document.Color = nil
	style.Text.Color = nil
	style.Heading.Color = ansiColor(colorAccent)
	style.H1.Color = ansiColor(colorAccent)
	style.H1.BackgroundColor = nil
	style.H2.Color = ansiColor(colorAccent)
	style.H3.Color = ansiColor(colorAccent)
	style.H4.Color = ansiColor(colorAccent)
	style.H5.Color = ansiColor(colorAccent)
	style.H6.Color = ansiColor(colorAccent)
	style.BlockQuote.Color = ansiColor(colorMuted)
	style.Item.Color = ansiColor(colorSuccess)
	style.Enumeration.Color = ansiColor(colorSuccess)

	style.Enumeration.BlockPrefix = ") "
	style.Task.Color = ansiColor(colorSuccess)
	style.Emph.Color = ansiColor(colorWarning)
	style.Strong.Color = ansiColor(colorWarning)
	style.HorizontalRule.Color = ansiColor(colorMuted)
	style.Link.Color = ansiColor(colorAccent)
	style.LinkText.Color = ansiColor(colorWarning)
	style.Image.Color = ansiColor(colorAccent)
	style.ImageText.Color = ansiColor(colorMuted)
	style.Code.Color = ansiColor(colorWarning)
	style.Code.BackgroundColor = nil
	style.CodeBlock.Color = nil

	style.CodeBlock.Chroma = nil
	style.Table.Color = nil
	style.DefinitionTerm.Color = ansiColor(colorAccent)
	style.DefinitionDescription.Color = nil
	style.HTMLBlock.Color = ansiColor(colorMuted)
	style.HTMLSpan.Color = ansiColor(colorMuted)

	return style
}()

func ansiColor(color lipgloss.Color) *string {
	return xptr.Ptr(string(color))
}

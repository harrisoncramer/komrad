package ui

import (
	"weave/lib/xptr"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// The panel's palette, and the two themes derived from it. Every colour is one of the
// sixteen a terminal's own scheme maps, written as its index rather than as a hex value, so
// the panel draws in whatever colours the terminal is already running rather than a hex
// scheme of its own that would look the same on every machine and right on almost none.
// Body text names no colour at all, so it draws in the terminal's own foreground.
const (
	// colorAccent is blue: headers and the cursor.
	colorAccent = lipgloss.Color("4")
	// colorSuccess is bright green: an armed schedule.
	colorSuccess = lipgloss.Color("10")
	// colorWarning is bright yellow: an agent still thinking, and a matched search term.
	colorWarning = lipgloss.Color("11")
	// colorDanger is bright red: an agent waiting on you. It is the red errui headlines
	// in, so a row needing attention and the screen explaining a failure agree.
	colorDanger = lipgloss.Color("9")
	// colorMuted is bright black, the sixteen's only grey. Secondary text, an idle agent,
	// and the chrome with no text in it all take it, since there is no second grey to tell
	// them apart.
	colorMuted = lipgloss.Color("8")
)

// formTheme dresses huh in the palette. huh otherwise falls back to its own Charm theme,
// whose indigo and fuchsia are hex and so belong to no scheme the terminal is running.
// ThemeBase is the starting point because it carries the layout and already draws what
// little it colours from the sixteen.
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

	// A blurred field keeps the focused layout and gives up the accent, so which field
	// the keys are going to is the one thing colour says on a form.
	theme.Blurred = theme.Focused
	theme.Blurred.Base = theme.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	theme.Blurred.Card = theme.Blurred.Base
	theme.Blurred.Title = theme.Blurred.Title.Foreground(colorMuted).Bold(false)
	theme.Blurred.NoteTitle = theme.Blurred.NoteTitle.Foreground(colorMuted).Bold(false)
	theme.Blurred.TextInput.Prompt = theme.Blurred.TextInput.Prompt.Foreground(colorMuted)
	theme.Blurred.NextIndicator = lipgloss.NewStyle()
	theme.Blurred.PrevIndicator = lipgloss.NewStyle()

	// The help line under a form comes from bubbles, which colours it in hex, so it is
	// restated here rather than inherited.
	theme.Help = helpStyles()

	return theme
}

// helpStyles is how bubbles draws the key help in the palette.
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

// stageStyle is how the stage renders markdown: glamour's dark style with every colour
// it names replaced by one of the sixteen, so a document in the stage and the rows beside
// it are drawn from the same palette. The layout, indents, and margins are left as they
// are, since those are the parts of the style worth keeping.
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
	// A record's numbered fields read as "1)" rather than glamour's own "1.", and are
	// dimmed by dimNumbers in stage_render.go after glamour renders, since glamour's
	// own marker colour never reaches this text (see the comment on numberStyle).
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
	// A fenced block is left unhighlighted. glamour hands the colours under Chroma to
	// chroma itself, which reads hex and nothing else, so an index reaches it as a style
	// element it does not know and panics the render. The block keeps its indent, which
	// is what sets it apart from the prose around it.
	style.CodeBlock.Chroma = nil
	style.Table.Color = nil
	style.DefinitionTerm.Color = ansiColor(colorAccent)
	style.DefinitionDescription.Color = nil
	style.HTMLBlock.Color = ansiColor(colorMuted)
	style.HTMLSpan.Color = ansiColor(colorMuted)

	return style
}()

// ansiColor is a palette colour in the form glamour takes it, which is a pointer to the
// index as a string.
func ansiColor(color lipgloss.Color) *string {
	return xptr.Ptr(string(color))
}

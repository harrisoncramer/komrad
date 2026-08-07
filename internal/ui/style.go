package ui

import (
	"fmt"

	"weave/config"
	"weave/internal/errui"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/lipgloss"
)

// How a row is drawn: its state, its tags, and the chrome around them. Every glyph
// here is single-width, since an emoji one renders double in some terminals and pushes
// the column out of line.

var (
	headerStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle    = errui.Dim
	// blurredHeaderStyle and blurredCursorStyle are the same two without the accent, which
	// is what a half of the sidebar draws in while the other half holds the keys. They keep
	// the weight, so a title is still a title and a cursor is still findable: only the
	// colour says where a keypress would land.
	blurredHeaderStyle = lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	blurredCursorStyle = lipgloss.NewStyle().Bold(true)
)

// titleStyle is how the list's own title is drawn: in the accent while the list has the keys,
// and grey while the details under it do.
func (m model) titleStyle() lipgloss.Style {
	return titleStyleFor(!m.details.focused)
}

// rowCursorStyle is how the marker beside the row under the list cursor is drawn. The row
// stays marked while the details below hold the keys, since it is the row they are about, but
// it gives up the accent to them.
func (m model) rowCursorStyle() lipgloss.Style {
	return cursorStyleFor(!m.details.focused)
}

// titleStyleFor and cursorStyleFor are the two halves of the sidebar asking the same question,
// so a title and a cursor never disagree about which half has the keys.
func titleStyleFor(focused bool) lipgloss.Style {
	if focused {
		return headerStyle
	}

	return blurredHeaderStyle
}

func cursorStyleFor(focused bool) lipgloss.Style {
	if focused {
		return cursorStyle
	}

	return blurredCursorStyle
}

func stateIcon(state string) string {
	switch state {
	case agentstore.StateWaiting:
		return "!"
	case agentstore.StateThinking:
		return "…"
	default:
		return "•"
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case agentstore.StateWaiting:
		return colorDanger
	case agentstore.StateThinking:
		return colorWarning
	default:
		return colorMuted
	}
}

const workflowIcon = "⧉"

func archivedTag(w workflowstore.Workflow) string {
	if w.ArchivedAt == nil {
		return ""
	}

	return " " + dimStyle.Render("archived")
}

const scheduledIcon = "◷"

func scheduledMark(agent agentstore.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	return " " + dimStyle.Render(scheduledIcon)
}

const hiddenIcon = "◌"

func hiddenMark(hidden bool) string {
	if !hidden {
		return ""
	}

	return " " + dimStyle.Render(hiddenIcon)
}

const unreadMailIcon = "✉"

func unreadMailMark(unread bool) string {
	if !unread {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(colorAccent).Render(unreadMailIcon)
}

func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

func tagGlyph(agent agentstore.Agent, tags []config.Tag) string {
	if agent.Tag == nil {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(tagColor(*agent.Tag, tags)).Render(*agent.Tag)
}

var taggedColor = lipgloss.Color(config.GetColor(config.DefaultTagColor))

func tagColor(glyph string, tags []config.Tag) lipgloss.Color {
	for _, tag := range tags {
		if tag.Glyph == glyph && tag.Color != "" {
			return lipgloss.Color(config.GetColor(tag.Color))
		}
	}

	return taggedColor
}

func scheduleIcon(enabled bool) string {
	if enabled {
		return "◆"
	}

	return "◇"
}

func scheduleColor(enabled bool) lipgloss.Color {
	if enabled {
		return colorSuccess
	}

	return colorMuted
}

var matchStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)

var brokenStyle = lipgloss.NewStyle().Foreground(colorDanger)

func (m model) nameStyle(hovered, inactive bool) lipgloss.Style {
	switch {
	case hovered:
		return m.rowCursorStyle()
	case inactive:
		return dimStyle
	default:
		return lipgloss.NewStyle()
	}
}

const nameWidth = 30

func clipName(name string) string {
	return clip(name, nameWidth)
}

func clip(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}

	return string(runes[:width-1]) + "…"
}

const selectionIcon = "▌"

var selectionStyle = lipgloss.NewStyle().Foreground(colorSuccess)

func selectionGutter(selected bool) string {
	if !selected {
		return "  "
	}

	return selectionStyle.Render(selectionIcon) + " "
}

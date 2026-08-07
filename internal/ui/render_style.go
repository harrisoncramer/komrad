package ui

import (
	"fmt"

	"weave/config"
	"weave/internal/errui"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/lipgloss"
)

// How a row is drawn: its state, its marks, and the chrome around them. Every glyph
// here is single-width, since an emoji one renders double in some terminals and pushes
// the column out of line.

var (
	headerStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle    = errui.Dim
)

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

func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

func markGlyph(agent agentstore.Agent, marks []config.Mark) string {
	if agent.Mark == nil {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(markColor(*agent.Mark, marks)).Render(*agent.Mark)
}

var markedColor = lipgloss.Color(config.GetColor(config.DefaultMarkColor))

func markColor(glyph string, marks []config.Mark) lipgloss.Color {
	for _, mark := range marks {
		if mark.Glyph == glyph && mark.Color != "" {
			return lipgloss.Color(config.GetColor(mark.Color))
		}
	}

	return markedColor
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

func nameStyle(hovered, inactive bool) lipgloss.Style {
	switch {
	case hovered:
		return cursorStyle
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

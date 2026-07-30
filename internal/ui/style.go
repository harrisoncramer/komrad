package ui

import (
	"fmt"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/errui"

	agentstore "claude-agents/internal/store/agent"

	"github.com/charmbracelet/lipgloss"
)

// What the list draws a row's state, marks, and chrome in. The glyphs are all
// single-width: an emoji one renders double in some terminals and pushes the column
// out of line.

var (
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	// dimStyle is shared with the error screens rather than restated here, so the
	// panel's secondary text and theirs cannot drift to different greys.
	dimStyle = errui.Dim
)

// markedColor is what a mark is drawn in when its config names no colour of its own.
const markedColor = lipgloss.Color(appconfig.DefaultMarkColor)

func stateIcon(state string) string {
	switch state {
	case agentstore.StateDone:
		return "✓"
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
	case agentstore.StateDone:
		return lipgloss.Color("#98BB6C")
	case agentstore.StateWaiting:
		return lipgloss.Color("#E82424")
	case agentstore.StateThinking:
		return lipgloss.Color("#FF9E3B")
	default:
		return lipgloss.Color("#7E9CD8")
	}
}

// scheduledIcon is a clock face rather than an emoji one, which would render
// double-width in some terminals and push the column out of line.
const scheduledIcon = "◷"

// scheduledMark is the clock rendered after a scheduled agent's name. One a person
// launched carries nothing, since that is the ordinary case and a marker on every
// row would say nothing.
func scheduledMark(agent agentstore.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	return " " + dimStyle.Render(scheduledIcon)
}

// hiddenIcon is a dotted circle, single-width for the same reason the clock is.
const hiddenIcon = "◌"

// hiddenMark is drawn after a hidden row's name, which is only on screen at all while
// the hidden ones are being shown. Without it a revealed row is indistinguishable
// from the rest and the hide key reads as doing nothing. Agents and schedules both
// draw it, since hiding means the same thing on either screen.
func hiddenMark(hidden bool) string {
	if !hidden {
		return ""
	}

	return " " + dimStyle.Render(hiddenIcon)
}

// hiddenTally is the count of hidden agents drawn after a workflow's name, so a
// folder says how much of itself it is not showing. It is dropped once they are on
// screen, where they are rows of their own and the count would only say it twice.
func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

// markGlyph is the glyph the user marked an agent with, drawn after the row rather
// than in the state column so it reads as their own mark and not another lifecycle
// state. It takes the colour that mark carries in the config.
func markGlyph(agent agentstore.Agent, marks []appconfig.Mark) string {
	if agent.Mark == nil {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(markColor(*agent.Mark, marks)).Render(*agent.Mark)
}

// markColor is the colour a glyph is drawn in. One the config does not list, or one
// listed with no colour of its own, falls back rather than going undrawn.
func markColor(glyph string, marks []appconfig.Mark) lipgloss.Color {
	for _, mark := range marks {
		if mark.Glyph == glyph && mark.Color != "" {
			return lipgloss.Color(mark.Color)
		}
	}

	return markedColor
}

// scheduleIcon marks whether a schedule is armed. A paused one keeps its row,
// so the list has to say which of the two it is at a glance.
func scheduleIcon(enabled bool) string {
	if enabled {
		return "◆"
	}

	return "◇"
}

func scheduleColor(enabled bool) lipgloss.Color {
	if enabled {
		return lipgloss.Color("#98BB6C")
	}

	return lipgloss.Color("#727169")
}

// matchStyle highlights the matched terms inside a result row's snippet, the
// panel-side rendering of the markers the store wraps hits in.
var matchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9E3B")).Bold(true)

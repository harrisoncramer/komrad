package ui

import (
	"fmt"

	"weave/internal/appconfig"
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
	// dimStyle is shared with the error screens rather than restated here, so the
	// panel's secondary text and theirs cannot drift to different greys.
	dimStyle = errui.Dim
)

// Only the two states worth looking at draw an icon of their own. An idle agent takes
// the neutral bullet, so the column reads as the few rows wanting attention against a
// quiet background rather than as a glyph per row to be told apart.
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

// workflowIcon is the folder a workflow header is drawn with on both trees.
const workflowIcon = "⧉"

// archivedTag is drawn after an archived workflow's name. The archive lists live folders
// alongside archived ones, and restore acts on only one of the two, so without it the
// rows read the same.
func archivedTag(w workflowstore.Workflow) string {
	if w.ArchivedAt == nil {
		return ""
	}

	return " " + dimStyle.Render("archived")
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

// markedColor is what a mark is drawn in when its config names no colour of its own.
var markedColor = lipgloss.Color(appconfig.GetColor(appconfig.DefaultMarkColor))

// markColor is the colour a glyph is drawn in. One the config does not list, or one
// listed with no colour of its own, falls back rather than going undrawn. The colour goes
// through appconfig on the way out, since a config names one of the sixteen by name and
// lipgloss knows it only by index.
func markColor(glyph string, marks []appconfig.Mark) lipgloss.Color {
	for _, mark := range marks {
		if mark.Glyph == glyph && mark.Color != "" {
			return lipgloss.Color(appconfig.GetColor(mark.Color))
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
		return colorSuccess
	}

	return colorMuted
}

// matchStyle highlights the matched terms inside a result row's snippet, the
// panel-side rendering of the markers the store wraps hits in.
var matchStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)

// selectionIcon is the bar a selected row carries, in green rather than the cursor's blue
// so a row that is both chosen and hovered says both at once.
const selectionIcon = "▌"

var selectionStyle = lipgloss.NewStyle().Foreground(colorSuccess)

// selectionGutter is the column down the left of the tree. It holds the bar or a blank of
// the same width, and it is drawn on every row whether or not anything is selected, since a
// gutter that opened with the first selection would push every row sideways.
func selectionGutter(selected bool) string {
	if !selected {
		return "  "
	}

	return selectionStyle.Render(selectionIcon) + " "
}

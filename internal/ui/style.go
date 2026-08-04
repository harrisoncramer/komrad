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
func markGlyph(agent agentstore.Agent, marks []config.Mark) string {
	if agent.Mark == nil {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(markColor(*agent.Mark, marks)).Render(*agent.Mark)
}

// markedColor is what a mark is drawn in when its config names no colour of its own.
var markedColor = lipgloss.Color(config.GetColor(config.DefaultMarkColor))

// markColor is the colour a glyph is drawn in. One the config does not list, or one
// listed with no colour of its own, falls back rather than going undrawn. The colour goes
// through appconfig on the way out, since a config names one of the sixteen by name and
// lipgloss knows it only by index.
func markColor(glyph string, marks []config.Mark) lipgloss.Color {
	for _, mark := range marks {
		if mark.Glyph == glyph && mark.Color != "" {
			return lipgloss.Color(config.GetColor(mark.Color))
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
// panel-side rendering of the markers the store wraps hits in. The filter draws what its
// pattern matched in the same colour, since both are a match under what was typed.
var matchStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)

// brokenStyle draws a filter pattern that will not compile, which is the only thing on screen
// saying why a list that should have narrowed did not.
var brokenStyle = lipgloss.NewStyle().Foreground(colorDanger)

// nameStyle is how a row's name is drawn. A hovered row takes the cursor's blue whichever row
// it is, and one the screen draws as inactive takes the grey. Every list reads its name style
// from here, since the filter renders a name in pieces to colour its match and needs the style
// the row would otherwise have had.
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

// nameWidth is how much of an agent's name a list row draws. An agent named after the
// branch it works on carries a whole ticket title, which is the name a worktree wants and
// far more than a row has room for, so the list shows the head of it and the record pane
// shows the whole thing.
const nameWidth = 30

// clipName cuts a name down to nameWidth, ending in an ellipsis where anything was
// dropped. Filtering still reads the whole name, so a row can match on a word the list
// does not draw.
func clipName(name string) string {
	return clip(name, nameWidth)
}

// descriptionWidth is how much of what an action says about itself a picker option draws,
// clipped so that no option grows taller than the others.
const descriptionWidth = 44

// clip cuts text down to width, ending in an ellipsis where anything was dropped.
func clip(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}

	return string(runes[:width-1]) + "…"
}

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

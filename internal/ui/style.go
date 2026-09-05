package ui

import (
	"fmt"

	"weave/config"
	"weave/internal/errui"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle        = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cursorStyle        = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle           = errui.Dim
	blurredHeaderStyle = lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	blurredCursorStyle = lipgloss.NewStyle().Bold(true)
)

func (m model) titleStyle() lipgloss.Style {
	return titleStyleFor(!m.isDetailsFocused())
}

func (m model) rowCursorStyle() lipgloss.Style {
	return cursorStyleFor(!m.isDetailsFocused())
}

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

// Both switches below carry no default, so a state added later fails the exhaustive check here
// rather than quietly drawing as an idle row.
func stateIcon(state agentstore.State) string {
	switch state {
	case agentstore.StateIdle:
		return "•"
	case agentstore.StateWaiting:
		return "!"
	case agentstore.StateThinking:
		return "…"
	case agentstore.StateUncertain:
		return "?"
	case agentstore.StateSettingUp:
		return "○"
	case agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return "✗"
	}

	return "•"
}

func stateColor(state agentstore.State) lipgloss.Color {
	switch state {
	case agentstore.StateIdle, agentstore.StateSettingUp, agentstore.StateUncertain:
		return colorMuted
	case agentstore.StateWaiting, agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return colorDanger
	case agentstore.StateThinking:
		return colorWarning
	}

	return colorMuted
}

func stateMark(state agentstore.State) string {
	return lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
}

const unviewedIcon = "●"

// unviewedMark is its own glyph rather than a colour on the state icon, since what an agent is
// doing and whether it has said something nobody has read are two facts either of which can be
// true alone.
func unviewedMark(unviewed bool) string {
	if !unviewed {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(colorSuccess).Render(unviewedIcon)
}

// noStateIcon fills the column the state icon would take for an agent that has no live state to
// report, since a header that leaves it empty sits at the indent its own snippets do.
const noStateIcon = "▫"

func noStateMark() string {
	return dimStyle.Render(noStateIcon)
}

// failedMark is the archive's only state icon, since an archived agent's last live state says
// nothing but a failed create or cleanup is why the row is still there.
func failedMark(agent agentstore.Agent) string {
	if !agent.State.IsFailed() {
		return ""
	}

	return " " + stateMark(agent.State)
}

// reasonWidth is how much of a reported reason a row draws, which is room for a tool name and
// not so much that a row stops being a column of names.
const reasonWidth = 24

// reasonMark is what the agent's own CLI last reported it was doing, drawn last on the row since
// it is the only part of one that is prose rather than a glyph.
func reasonMark(agent agentstore.Agent) string {
	if agent.ReportedReason == "" {
		return ""
	}

	return " " + dimStyle.Render(clip(agent.ReportedReason, reasonWidth))
}

const workflowIcon = "⧉"

const childIcon = "└"

// childMark opens the column a child's state mark is pushed right by, drawn where its parent's
// own mark sits so that every parent stays aligned with every other one.
func childMark(child bool) string {
	if !child {
		return ""
	}

	return dimStyle.Render(childIcon) + " "
}

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

const failedRunIcon = "⚑"

// failedRunMark says a script weave fired for the agent failed and nobody has read it, which
// the state icon cannot carry because a hook can fail while the agent itself is working away.
func failedRunMark(failed bool) string {
	if !failed {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(colorDanger).Render(failedRunIcon)
}

const worktreeRemovedIcon = "⊘"

func worktreeRemovedMark(agent agentstore.Agent) string {
	if !agent.WorktreeRemoved {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(colorDanger).Render(worktreeRemovedIcon)
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

// stageMatchStyle marks a match in rendered markdown, where the warning colour the listing marks
// with is already spoken for by inline code, so the word takes a block behind it instead.
var stageMatchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colorMuted)

var brokenStyle = lipgloss.NewStyle().Foreground(colorDanger)

func (m model) nameStyle(hovered, selected, inactive bool) lipgloss.Style {
	switch {
	case selected:
		return selectionStyle
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

// selectionStyle is how a row taken into a selection is drawn, which is the name itself rather
// than a mark beside it, and beats the cursor so that the row under the cursor still shows
// whether it is in.
var selectionStyle = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)

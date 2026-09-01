package ui

import (
	"fmt"
	"strings"
	"time"

	"weave/config"
	"weave/internal/errui"
	"weave/lib/dates"

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
	return titleStyleFor(!m.detailsFocused())
}

func (m model) rowCursorStyle() lipgloss.Style {
	return cursorStyleFor(!m.detailsFocused())
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

func stateIcon(state string) string {
	switch state {
	case agentstore.StateWaiting:
		return "!"
	case agentstore.StateThinking:
		return "…"
	case agentstore.StateSettingUp:
		return "○"
	case agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return "✗"
	default:
		return "•"
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case agentstore.StateWaiting, agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return colorDanger
	case agentstore.StateThinking:
		return colorWarning
	default:
		return colorMuted
	}
}

func stateMark(state string) string {
	return stateMarkFor(state, false)
}

// stateColorFor brightens the idle dot on an agent that has finished something nobody has looked
// at since, leaving a working agent reading as working whether or not it left anything behind.
func stateColorFor(state string, unviewed bool) lipgloss.Color {
	if unviewed && state == agentstore.StateIdle {
		return colorSuccess
	}

	return stateColor(state)
}

func stateMarkFor(state string, unviewed bool) string {
	return lipgloss.NewStyle().Foreground(stateColorFor(state, unviewed)).Render(stateIcon(state))
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
	if !agentstore.IsFailed(agent.State) {
		return ""
	}

	return " " + stateMark(agent.State)
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

const (
	// ghostColumn is where the ghost slot opens: the fixed gutter and cursor, the clipped name,
	// and room for every icon a row can wear, so the slot never lands on top of one.
	ghostColumn = 6 + nameWidth + 2*maxRowIcons

	// ghostWidth is the slot itself, wide enough for the longest span Elapsed writes.
	ghostWidth = 4

	// maxRowIcons is how many icons follow a name: the removed worktree, the schedule, the
	// hidden mark, the tag, and unread mail.
	maxRowIcons = 5
)

// ghostSlot pads a row out to the ghost column and right-aligns the text in it, drawing nothing
// in a pane too narrow to hold both, where pulling the slot in would only stack it onto the icons.
func ghostSlot(row, text string, width int) string {
	if text == "" || width < ghostColumn+ghostWidth+2 {
		return row
	}

	pad := max(ghostColumn-lipgloss.Width(row), 2)

	return row + strings.Repeat(" ", pad) + dimStyle.Render(fmt.Sprintf("%*s", ghostWidth, text))
}

// ghostText is what the slot says about an agent, which for now is how long it has been in the
// state it is in.
func ghostText(item agentItem) string {
	if item.agent.StateChangedAt.IsZero() {
		return ""
	}

	return dates.Elapsed(time.Since(item.agent.StateChangedAt))
}

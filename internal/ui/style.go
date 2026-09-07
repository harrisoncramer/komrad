package ui

import (
	"fmt"
	"strings"

	"weave/internal/errui"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/lipgloss"
)

const (
	unviewedIcon        = "●"
	childIcon           = "└"
	scheduledIcon       = "◷"
	hiddenIcon          = "◌"
	unreadMailIcon      = "✉"
	failedRunIcon       = "⚑"
	reviewRoundsIcon    = "⟳"
	worktreeRemovedIcon = "⊘"
	workflowIcon        = "⧉"

	// noStateIcon fills the column the state icon would take for a row with no live state to
	// report, so that row keeps the indent every other row has.
	noStateIcon = "▫"

	// reasonWidth is how much of a reported reason a row draws, which is room for a tool name and
	// not so much that a row stops being a column of names.
	reasonWidth = 24
	nameWidth   = 30
	ansiReset   = "\x1b[0m"
)

var (
	headerStyle        = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	cursorStyle        = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	dimStyle           = errui.Dim
	blurredHeaderStyle = lipgloss.NewStyle().Foreground(colorMuted).Bold(true)
	blurredCursorStyle = lipgloss.NewStyle().Bold(true)
)

func (m model) titleStyle() lipgloss.Style {
	return titleStyleFor(m.isListFocused())
}

func (m model) rowCursorStyle() lipgloss.Style {
	return cursorStyleFor(m.isListFocused())
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

// iconMark is the shape every optional mark on a row takes: nothing at all when the fact it
// stands for is not true, and otherwise a space and the mark in its own colour.
func iconMark(show bool, icon string, color lipgloss.Color) string {
	if !show {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(color).Render(icon)
}

// unviewedMark is its own glyph rather than a colour on the state icon, since either fact can
// be true without the other.
func unviewedMark(unviewed bool) string {
	return iconMark(unviewed, unviewedIcon, colorSuccess)
}

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

// reasonMark is what the agent's own CLI last reported it was doing, drawn last on the row since
// it is the only part of one that is prose rather than a glyph.
func reasonMark(agent agentstore.Agent) string {
	return iconMark(agent.ReportedReason != "", clip(agent.ReportedReason, reasonWidth), colorMuted)
}

// childMark draws in the column its parent's own state mark sits in, pushing the rest of the
// child's row across so every parent stays aligned with every other one.
func childMark(child bool) string {
	if !child {
		return ""
	}

	return dimStyle.Render(childIcon) + " "
}

func archivedTag(w workflowstore.Workflow) string {
	return iconMark(w.ArchivedAt != nil, "archived", colorMuted)
}

func scheduledMark(agent agentstore.Agent) string {
	return iconMark(agent.ScheduleID != nil, scheduledIcon, colorMuted)
}

func hiddenMark(hidden bool) string {
	return iconMark(hidden, hiddenIcon, colorMuted)
}

func unreadMailMark(unread bool) string {
	return iconMark(unread, unreadMailIcon, colorAccent)
}

func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

// reviewRoundsMark counts the rounds of review an agent's work has been through, which is drawn
// as a number because a change going round again and again is what it is there to show.
func reviewRoundsMark(rounds int32) string {
	return iconMark(rounds > 0, fmt.Sprintf("%s%d", reviewRoundsIcon, rounds), colorMuted)
}

// failedRunMark says a script weave fired for the agent failed and nobody has read it, which
// the state icon cannot carry because a hook can fail while the agent itself is working away.
func failedRunMark(failed bool) string {
	return iconMark(failed, failedRunIcon, colorDanger)
}

func worktreeRemovedMark(agent agentstore.Agent) string {
	return iconMark(agent.WorktreeRemoved, worktreeRemovedIcon, colorDanger)
}

// markGutter is the column a mark is drawn in, left of every row, so the letters a person
// addresses rows by read down one edge rather than being hunted for along a row.
func markGutter(mark string) string {
	if mark == "" {
		return "  "
	}

	return lipgloss.NewStyle().Foreground(colorMark).Render(mark) + " "
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

// selectionBackground is the bar a row taken into a selection is drawn under, dark enough that
// the colours the row is already carrying still read through it, so a selected row keeps its
// state icon and its cursor rather than trading them for the fact that it is in.
var selectionBackground = lipgloss.AdaptiveColor{Light: "#cdf0d6", Dark: "#20452c"}

// paintRow lays a background under one already-coloured line, setting it again after every reset
// that line carries, since the first of those would otherwise drop it for the rest of the row,
// and filling the line out to the pane so that it reads as a bar, which is measured across the
// whole string it is handed and so holds only for a row of a single line.
func paintRow(row string, background lipgloss.AdaptiveColor, width int) string {
	// The diff viewer threads a background into every fragment it draws instead, since its gutter
	// and its match highlight each carry one of their own, where a row wants the one background
	// under all of it and the fragments know nothing about it.
	set := backgroundSequence(background)
	if set == "" {
		return row
	}

	painted := set + strings.ReplaceAll(row, ansiReset, ansiReset+set)
	if padding := width - lipgloss.Width(row); padding > 0 {
		painted += strings.Repeat(" ", padding)
	}

	return painted + ansiReset
}

// backgroundSequence is the escape that sets a background and nothing else, which lipgloss will
// only write around text of its own, and is empty under a profile carrying no colour so that
// paintRow leaves such a row alone rather than padding it out to a bar nobody can see.
func backgroundSequence(background lipgloss.AdaptiveColor) string {
	hex := background.Light
	if lipgloss.HasDarkBackground() {
		hex = background.Dark
	}

	sequence := lipgloss.ColorProfile().Color(hex).Sequence(true)
	if sequence == "" {
		return ""
	}

	return "\x1b[" + sequence + "m"
}

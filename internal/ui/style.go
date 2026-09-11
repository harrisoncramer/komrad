package ui

import (
	"fmt"

	"weave/internal/errui"
	"weave/internal/ui/paint"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/lipgloss"
)

const (
	unviewedIcon              = "●"
	childIcon                 = "└"
	scheduledIcon             = "◷"
	hiddenIcon                = "◌"
	unreadMailIcon            = "✉"
	failedRunIcon             = "⚑"
	localReviewRoundsIcon     = "⟳"
	inLocalReviewIcon         = "✎"
	localApprovedIcon         = "✓"
	localChangesRequestedIcon = "↩"
	worktreeRemovedIcon       = "⊘"
	workflowIcon              = "⧉"

	// noStateIcon fills the column the state icon would take for a row with no live state to
	// report, keeping the indent every other row has.
	noStateIcon = "▫"

	// reasonWidth is how much of a reported reason a row draws: room for a tool name, and not so
	// much that a row stops being a column of names.
	reasonWidth = 24
	nameWidth   = 30
)

var dimStyle = errui.Dim

func (m model) titleStyle() lipgloss.Style {
	return paint.TitleStyleFor(m.isListFocused())
}

func (m model) rowCursorStyle() lipgloss.Style {
	return paint.CursorStyleFor(m.isListFocused())
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
		return paint.Muted
	case agentstore.StateWaiting, agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return paint.Danger
	case agentstore.StateThinking:
		return paint.Warning
	}

	return paint.Muted
}

func stateMark(state agentstore.State) string {
	return lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
}

// unviewedMark is its own glyph rather than a colour on the state icon.
func unviewedMark(unviewed bool) string {
	return paint.IconMark(unviewed, unviewedIcon, paint.Success)
}

func noStateMark() string {
	return dimStyle.Render(noStateIcon)
}

// failedMark is the archive's only state icon.
func failedMark(agent agentstore.Agent) string {
	if !agent.State.IsFailed() {
		return ""
	}

	return " " + stateMark(agent.State)
}

// reasonMark is what the agent's own CLI last reported it was doing, drawn last on the row since
// it is the only part of one that is prose rather than a glyph.
func reasonMark(agent agentstore.Agent) string {
	return paint.IconMark(agent.ReportedReason != "", paint.Clip(agent.ReportedReason, reasonWidth), paint.Muted)
}

// childMark draws in the column its parent's own state mark sits in, pushing the rest of the
// child's row across so every parent stays aligned.
func childMark(child bool) string {
	if !child {
		return ""
	}

	return dimStyle.Render(childIcon) + " "
}

func archivedTag(w workflowstore.Workflow) string {
	return paint.IconMark(w.ArchivedAt != nil, "archived", paint.Muted)
}

func scheduledMark(agent agentstore.Agent) string {
	return paint.IconMark(agent.ScheduleID != nil, scheduledIcon, paint.Muted)
}

func hiddenMark(hidden bool) string {
	return paint.IconMark(hidden, hiddenIcon, paint.Muted)
}

func unreadMailMark(unread bool) string {
	return paint.IconMark(unread, unreadMailIcon, paint.Accent)
}

func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

// localReviewRoundsMark counts the rounds of local review an agent's work has been through, drawn
// as a number.
func localReviewRoundsMark(rounds int32) string {
	return paint.IconMark(rounds > 0, fmt.Sprintf("%s%d", localReviewRoundsIcon, rounds), paint.Muted)
}

// localReviewMark is the single glyph a row draws about local review: the last released verdict
// when there is one, otherwise the pencil that says a reviewer is still reading.
func localReviewMark(inLocalReview bool, verdict string) string {
	icon, color := localVerdictIcon(verdict)
	if icon == "" {
		return paint.IconMark(inLocalReview, inLocalReviewIcon, paint.Accent)
	}

	return paint.IconMark(true, icon, color)
}

// localVerdictIcon is the glyph and colour a released local verdict wears.
func localVerdictIcon(verdict string) (string, lipgloss.Color) {
	switch reviewstore.Verdict(verdict) {
	case reviewstore.VerdictApproved:
		return localApprovedIcon, paint.Success
	case reviewstore.VerdictChangesRequested:
		return localChangesRequestedIcon, paint.Warning
	case reviewstore.VerdictCommented:
		return "", paint.Muted
	}

	return "", paint.Muted
}

// failedRunMark says a script weave fired for the agent failed and nobody has read it.
func failedRunMark(failed bool) string {
	return paint.IconMark(failed, failedRunIcon, paint.Danger)
}

// worktreeRemovedMark says the directory an agent was given is gone, and stays quiet for a
// failed create.
func worktreeRemovedMark(item agentItem) string {
	gone := item.agent.WorktreeRemoved && displayState(item) != agentstore.StateFailedToCreate

	return paint.IconMark(gone, worktreeRemovedIcon, paint.Danger)
}

func scheduleIcon(enabled bool) string {
	if enabled {
		return "◆"
	}

	return "◇"
}

func scheduleColor(enabled bool) lipgloss.Color {
	if enabled {
		return paint.Success
	}

	return paint.Muted
}

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
	return paint.Clip(name, nameWidth)
}

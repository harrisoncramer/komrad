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
	sequenceInProgressIcon    = "◑"
	heldIcon                  = "⏸"
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
	return m.palette(regionList).Header()
}

func (m model) rowCursorStyle() lipgloss.Style {
	return m.palette(regionList).Cursor()
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

func stateTone(state agentstore.State) paint.Tone {
	switch state {
	case agentstore.StateIdle, agentstore.StateSettingUp, agentstore.StateUncertain:
		return paint.ToneMuted
	case agentstore.StateWaiting, agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return paint.ToneDanger
	case agentstore.StateThinking:
		return paint.ToneWarning
	}

	return paint.ToneMuted
}

func stateMark(palette paint.Palette, state agentstore.State) string {
	return palette.Style(stateTone(state)).Render(stateIcon(state))
}

// unviewedMark is its own glyph rather than a colour on the state icon.
func unviewedMark(palette paint.Palette, unviewed bool) string {
	return palette.Mark(unviewed, unviewedIcon, paint.ToneSuccess)
}

func noStateMark() string {
	return dimStyle.Render(noStateIcon)
}

// failedMark is the archive's only state icon.
func failedMark(palette paint.Palette, agent agentstore.Agent) string {
	if !agent.State.IsFailed() {
		return ""
	}

	return " " + stateMark(palette, agent.State)
}

// reasonMark is what the agent's own CLI last reported it was doing, drawn last on the row since
// it is the only part of one that is prose rather than a glyph.
func reasonMark(palette paint.Palette, agent agentstore.Agent) string {
	return palette.Mark(agent.ReportedReason != "", paint.Clip(agent.ReportedReason, reasonWidth), paint.ToneMuted)
}

// childMark draws in the column its parent's own state mark sits in, pushing the rest of the
// child's row across so every parent stays aligned.
func childMark(child bool) string {
	if !child {
		return ""
	}

	return dimStyle.Render(childIcon) + " "
}

func archivedTag(palette paint.Palette, w workflowstore.Workflow) string {
	return palette.Mark(w.ArchivedAt != nil, "archived", paint.ToneMuted)
}

func scheduledMark(palette paint.Palette, agent agentstore.Agent) string {
	return palette.Mark(agent.ScheduleID != nil, scheduledIcon, paint.ToneMuted)
}

// heldMark says a person has paused this agent and has its worktree.
func heldMark(palette paint.Palette, agent agentstore.Agent) string {
	return palette.Mark(agent.HeldAt != nil, heldIcon, paint.ToneWarning)
}

func hiddenMark(palette paint.Palette, hidden bool) string {
	return palette.Mark(hidden, hiddenIcon, paint.ToneMuted)
}

func unreadMailMark(palette paint.Palette, unread bool) string {
	return palette.Mark(unread, unreadMailIcon, paint.ToneAccent)
}

func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

// localReviewRoundsMark counts the rounds of local review an agent's work has been through, drawn
// as a number.
func localReviewRoundsMark(palette paint.Palette, rounds int32) string {
	return palette.Mark(rounds > 0, fmt.Sprintf("%s%d", localReviewRoundsIcon, rounds), paint.ToneMuted)
}

// localReviewMark is the single glyph a row draws about local review: the last released verdict
// when there is one, otherwise the pencil that says a reviewer is still reading.
func localReviewMark(palette paint.Palette, inLocalReview bool, verdict string) string {
	icon, tone := localVerdictIcon(verdict)
	if icon == "" {
		return palette.Mark(inLocalReview, inLocalReviewIcon, paint.ToneAccent)
	}

	return palette.Mark(true, icon, tone)
}

// localVerdictIcon is the glyph and tone a released local verdict wears.
func localVerdictIcon(verdict string) (string, paint.Tone) {
	switch reviewstore.Verdict(verdict) {
	case reviewstore.VerdictApproved:
		return localApprovedIcon, paint.ToneSuccess
	case reviewstore.VerdictChangesRequested:
		return localChangesRequestedIcon, paint.ToneWarning
	case reviewstore.VerdictCommented:
		return "", paint.ToneMuted
	}

	return "", paint.ToneMuted
}

// failedRunMark says a script weave fired for the agent failed and nobody has read it.
func failedRunMark(palette paint.Palette, failed bool) string {
	return palette.Mark(failed, failedRunIcon, paint.ToneDanger)
}

// worktreeMark says the checkout an agent was given is not in a state it can be left in, and
// stays quiet for a failed create.
func worktreeMark(palette paint.Palette, item agentItem) string {
	if displayState(item) == agentstore.StateFailedToCreate {
		return ""
	}

	if item.agent.WorktreeRemoved {
		return palette.Mark(true, worktreeRemovedIcon, paint.ToneDanger)
	}

	return palette.Mark(item.agent.SequenceInProgress, sequenceInProgressIcon, paint.ToneWarning)
}

func scheduleIcon(enabled bool) string {
	if enabled {
		return "◆"
	}

	return "◇"
}

func scheduleTone(enabled bool) paint.Tone {
	if enabled {
		return paint.ToneSuccess
	}

	return paint.ToneMuted
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

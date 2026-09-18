package ui

import (
	"fmt"

	"weave/internal/errui"
	"weave/internal/ui/paint"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"

	"github.com/charmbracelet/lipgloss"
)

const (
	unviewedIcon              = "●"
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

	// noStateIcon fills the column the state icon would take for a row with no live state to
	// report, keeping the indent every other row has.
	noStateIcon = "▫"

	nameWidth = 30
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

// failedMark is a state icon drawn only for an agent whose launch or teardown failed.
func failedMark(palette paint.Palette, agent agentstore.Agent) string {
	if !agent.State.IsFailed() {
		return ""
	}

	return " " + stateMark(palette, agent.State)
}

func scheduledMark(palette paint.Palette, agent agentstore.Agent) string {
	return palette.Mark(agent.ScheduleID != nil, scheduledIcon, paint.ToneMuted)
}

func hiddenMark(palette paint.Palette, hidden bool) string {
	return palette.Mark(hidden, hiddenIcon, paint.ToneMuted)
}

func unreadMailMark(palette paint.Palette, unread bool) string {
	return palette.Mark(unread, unreadMailIcon, paint.ToneAccent)
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

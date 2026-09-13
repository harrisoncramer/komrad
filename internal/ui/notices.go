package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/ui/paint"

	"github.com/charmbracelet/lipgloss"
)

// notice is one of the marks a row draws said in words.
type notice struct {
	icon  string
	color lipgloss.Color
	text  string
}

const (
	failedToCreateNotice  = "setup failed, it never launched"
	failedToCleanupNotice = "teardown failed, its directory is still there"
	failedRunNotice       = "a script weave fired failed, log unread"
	unreadMailNotice      = "mail waiting, it has not read it"
	worktreeRemovedNotice = "its directory no longer exists"
	unviewedNotice        = "unread output in its pane"
	hiddenNotice          = "hidden from the sidebar"
	heldNotice            = "held: you stopped it and have its worktree"
)

const inLocalReviewNotice = "in review, reviewer owed an answer"

// localVerdictNotice says where the last released local review of an agent's newest round left
// it, and says nothing for a verdict that draws nothing.
func localVerdictNotice(verdict string) string {
	switch reviewstore.Verdict(verdict) {
	case reviewstore.VerdictApproved:
		return "local reviewer approved this round"
	case reviewstore.VerdictChangesRequested:
		return "local reviewer asked for changes"
	case reviewstore.VerdictCommented:
		return ""
	}

	return ""
}

// localReviewRoundsNotice counts against the agent holding the diff.
func localReviewRoundsNotice(rounds int32) string {
	if rounds == 1 {
		return "reviewed once"
	}

	return fmt.Sprintf("reviewed %d times", rounds)
}

// agentNotices orders the marks by how much each wants a person's attention.
func agentNotices(item agentItem) []notice {
	state := displayState(item)

	notices := failureNotices(state)
	notices = appendNotice(notices, item.failedRun, failedRunIcon, paint.Danger, failedRunNotice)
	notices = appendNotice(notices, item.unreadMail, unreadMailIcon, paint.Accent, unreadMailNotice)
	notices = appendNotice(notices, item.agent.WorktreeRemoved && state != agentstore.StateFailedToCreate, worktreeRemovedIcon, paint.Danger, worktreeRemovedNotice)
	notices = appendNotice(notices, item.unviewed, unviewedIcon, paint.Success, unviewedNotice)
	notices = appendNotice(notices, item.agent.HeldAt != nil, heldIcon, paint.Warning, heldNotice)
	notices = appendNotice(notices, item.agent.Hidden, hiddenIcon, paint.Muted, hiddenNotice)

	icon, color := localVerdictIcon(item.localVerdict)
	said := localVerdictNotice(item.localVerdict)
	if icon == "" || said == "" {
		notices = appendNotice(notices, item.inLocalReview, inLocalReviewIcon, paint.Accent, inLocalReviewNotice)
	}

	notices = appendNotice(notices, icon != "" && said != "", icon, color, said)

	return appendNotice(notices, item.localRounds > 0, localReviewRoundsIcon, paint.Muted, localReviewRoundsNotice(item.localRounds))
}

// failureNotices is the whole of what an archived row has to say.
func failureNotices(state agentstore.State) []notice {
	notices := appendNotice(nil, state == agentstore.StateFailedToCreate, stateIcon(state), stateColor(state), failedToCreateNotice)

	return appendNotice(notices, state == agentstore.StateFailedToCleanup, stateIcon(state), stateColor(state), failedToCleanupNotice)
}

func appendNotice(notices []notice, show bool, icon string, color lipgloss.Color, text string) []notice {
	if !show {
		return notices
	}

	return append(notices, notice{
		icon:  icon,
		color: color,
		text:  text,
	})
}

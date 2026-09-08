package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"

	"github.com/charmbracelet/lipgloss"
)

// notice is one of the marks a row draws said in words.
type notice struct {
	icon  string
	color lipgloss.Color
	text  string
}

const (
	failedToCreateNotice  = "setup failed: its worktree or a pre-launch hook did not finish"
	failedToCleanupNotice = "teardown failed: its worktree could not be removed, so the directory it ran in is still there"
	failedRunNotice       = "run failed: a script weave fired for this agent failed, and nobody has opened the log it went to"
	unreadMailNotice      = "mail waiting: another agent has written to this one and it has not read the mail yet"
	worktreeRemovedNotice = "worktree gone: the directory this agent was given no longer exists"
	unviewedNotice        = "unread output: it has said something since anybody last watched its pane"
	hiddenNotice          = "hidden: the sidebar leaves this row out until hidden agents are shown"
)

const inLocalReviewNotice = "in local review: a reviewer is reading this agent's work and is still owed an answer"

// localVerdictNotice says where the last released local review of an agent's newest round left it,
// and says nothing for a verdict that draws nothing, one released without a conclusion included.
func localVerdictNotice(verdict string) string {
	switch reviewstore.Verdict(verdict) {
	case reviewstore.VerdictApproved:
		return "approved locally: the last local review of its newest round signed that round off"
	case reviewstore.VerdictChangesRequested:
		return "changes requested locally: the last local review of its newest round asked for more"
	case reviewstore.VerdictCommented:
		return ""
	}

	return ""
}

// localReviewRoundsNotice counts against the agent holding the diff.
func localReviewRoundsNotice(rounds int32) string {
	if rounds == 1 {
		return "local review rounds: its work has been reviewed once"
	}

	return fmt.Sprintf("local review rounds: its work has been reviewed %d times", rounds)
}

// agentNotices orders the marks by how much each wants a person's attention.
func agentNotices(item agentItem) []notice {
	state := displayState(item)

	notices := failureNotices(state)
	notices = appendNotice(notices, item.failedRun, failedRunIcon, colorDanger, failedRunNotice)
	notices = appendNotice(notices, item.unreadMail, unreadMailIcon, colorAccent, unreadMailNotice)
	notices = appendNotice(notices, item.agent.WorktreeRemoved && state != agentstore.StateFailedToCreate, worktreeRemovedIcon, colorDanger, worktreeRemovedNotice)
	notices = appendNotice(notices, item.unviewed, unviewedIcon, colorSuccess, unviewedNotice)
	notices = appendNotice(notices, item.agent.Hidden, hiddenIcon, colorMuted, hiddenNotice)
	notices = appendNotice(notices, item.inLocalReview, inLocalReviewIcon, colorAccent, inLocalReviewNotice)

	icon, color := localVerdictIcon(item.localVerdict)
	said := localVerdictNotice(item.localVerdict)
	notices = appendNotice(notices, icon != "" && said != "", icon, color, said)

	return appendNotice(notices, item.localRounds > 0, localReviewRoundsIcon, colorMuted, localReviewRoundsNotice(item.localRounds))
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

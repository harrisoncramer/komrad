package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"

	"github.com/charmbracelet/lipgloss"
)

// notice is one of the marks a row draws said in words, since the glyph says a fact about the
// agent is true without saying which fact.
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

// reviewRoundsNotice says both directions, since the count on the row is the two of them added
// up and which one it stands for is what tells a loop from a reviewer that has read a lot.
func reviewRoundsNotice(item agentItem) string {
	return fmt.Sprintf("review rounds: %d given on its work, %d it has given", item.roundsReceived, item.roundsSent)
}

// agentNotices orders the marks by how much each wants a person's attention, since the record
// gives up the ones at the end of the list when the pane is short.
func agentNotices(item agentItem) []notice {
	notices := failureNotices(displayState(item))
	notices = appendNotice(notices, item.failedRun, failedRunIcon, colorDanger, failedRunNotice)
	notices = appendNotice(notices, item.unreadMail, unreadMailIcon, colorAccent, unreadMailNotice)
	notices = appendNotice(notices, item.agent.WorktreeRemoved, worktreeRemovedIcon, colorDanger, worktreeRemovedNotice)
	notices = appendNotice(notices, item.unviewed, unviewedIcon, colorSuccess, unviewedNotice)
	notices = appendNotice(notices, reviewRounds(item) > 0, reviewRoundsIcon, colorMuted, reviewRoundsNotice(item))

	return appendNotice(notices, item.agent.Hidden, hiddenIcon, colorMuted, hiddenNotice)
}

// failureNotices is the whole of what an archived row has to say, since every other mark stands
// for something that is only true of an agent still running.
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

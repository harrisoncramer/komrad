package ui

import (
	"strings"

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
	failedToCreateNotice  = "setup failed: its worktree or one of the hooks weave ran before launch did not finish, and the run log says why"
	failedToCleanupNotice = "teardown failed: its worktree could not be removed, so the directory it ran in is still there"
	worktreeRemovedNotice = "worktree gone: the directory this agent was given no longer exists"
	scheduledNotice       = "scheduled: a schedule launched this agent rather than a person"
	hiddenNotice          = "hidden: the sidebar leaves this row out until hidden agents are shown"
	unreadMailNotice      = "mail waiting: another agent has written to this one and it has not read the mail yet"
	unviewedNotice        = "unread output: it has said something since anybody last watched its pane"
	failedRunNotice       = "run failed: a script weave fired for this agent failed, and nobody has opened the log it went to"
)

func agentNotices(item agentItem) []notice {
	state := displayState(item)

	notices := failureNotices(state)
	notices = appendNotice(notices, item.agent.WorktreeRemoved, worktreeRemovedIcon, colorDanger, worktreeRemovedNotice)
	notices = appendNotice(notices, item.agent.ScheduleID != nil, scheduledIcon, colorMuted, scheduledNotice)
	notices = appendNotice(notices, item.agent.Hidden, hiddenIcon, colorMuted, hiddenNotice)
	notices = appendNotice(notices, item.unreadMail, unreadMailIcon, colorAccent, unreadMailNotice)
	notices = appendNotice(notices, item.unviewed, unviewedIcon, colorSuccess, unviewedNotice)

	return appendNotice(notices, item.failedRun, failedRunIcon, colorDanger, failedRunNotice)
}

// archiveNotices covers the two marks an archived row still draws, since the rest of them are
// facts about an agent that is running.
func archiveNotices(agent agentstore.Agent) []notice {
	notices := failureNotices(agent.State)

	return appendNotice(notices, agent.ScheduleID != nil, scheduledIcon, colorMuted, scheduledNotice)
}

func failureNotices(state agentstore.State) []notice {
	notices := appendNotice(nil, state == agentstore.StateFailedToCreate, stateIcon(state), stateColor(state), failedToCreateNotice)

	return appendNotice(notices, state == agentstore.StateFailedToCleanup, stateIcon(state), stateColor(state), failedToCleanupNotice)
}

func appendNotice(notices []notice, show bool, icon string, color lipgloss.Color, text string) []notice {
	if !show {
		return notices
	}

	return append(notices, notice{icon: icon, color: color, text: text})
}

// wrapText breaks prose onto lines that fit a width, which the record does for a notice and for
// nothing else, since every other line it draws is a value that reads better clipped.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	lines := []string{}
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}

	return append(lines, line)
}

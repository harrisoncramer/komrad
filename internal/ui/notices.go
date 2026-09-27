package ui

import (
	"fmt"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
)

// notice is one of the marks a row draws said in words, carrying the part it plays rather than a
// colour.
type notice struct {
	icon string
	tone paint.Tone
	text string
}

const (
	failedToCreateNotice  = "setup failed, it never launched"
	failedToCleanupNotice = "teardown failed, its directory is still there"
	failedRunNotice       = "a script komrad fired failed, log unread"
	unreadMailNotice      = "mail waiting, it has not read it"
	worktreeRemovedNotice = "its directory no longer exists"
	sequenceNotice        = "a rebase is stopped part way in it"
	hiddenNotice          = "hidden from the sidebar"
	heldNotice            = "paused: you stopped it and have its worktree"
	heldWorkingNotice     = "paused, but it has started working again"
	heldUnreadableNotice  = "paused, but its pane cannot be read, so it may be writing"
	heldPromptedNotice    = "paused, but stopped at a prompt, so a write lands if anybody answers"
)

var (
	// failureSaid is what the status column drawn broken says about how an agent got there.
	failureSaid = map[panel.State]string{
		panel.StateFailedToCreate:  failedToCreateNotice,
		panel.StateFailedToCleanup: failedToCleanupNotice,
	}

	// repoSaid is what the repo column says where it stands away from its baseline.
	repoSaid = map[view.Repo]string{
		view.RepoGone:     worktreeRemovedNotice,
		view.RepoRebasing: sequenceNotice,
	}
)

// agentNotices orders the marks by how much each wants a person's attention, reading the row the
// list would draw so a column the row leaves empty is a column the record says nothing about.
func agentNotices(r agentRow) []notice {
	item, row := r.item, snapshotRow(r, snapshotParams{})

	notices := appendCellNotice(nil, view.StatusCell(row), failureSaid[displayState(item)])
	notices = appendCellNotice(notices, view.AlertCell(row), failedRunNotice)
	notices = appendCellNotice(notices, view.MailCell(row), unreadMailNotice)
	notices = appendCellNotice(notices, view.RepoCell(row), repoSaid[row.Repo])
	notices = appendCellNotice(notices, view.HeldCell(row), heldNoticeText(item))
	notices = appendCellNotice(notices, view.HiddenCell(row), hiddenNotice)
	notices = appendCommentsNotice(notices, row.ReviewerComments, "reviewer agents' comments")

	return appendCommentsNotice(notices, row.PersonComments, "your comments")
}

// appendCommentsNotice says how one side's comments stand, marked with the dot while any of
// them holds something unread.
func appendCommentsNotice(notices []notice, comments *view.Comments, whose string) []notice {
	if comments == nil {
		return notices
	}

	text := fmt.Sprintf("%s: %d open, %d resolved", whose, comments.Open, comments.Resolved)
	if !comments.Unread {
		return appendNotice(notices, true, "", paint.ToneMuted, text)
	}

	return appendNotice(notices, true, "·", paint.ToneWarning, text+", with something you have not read")
}

// appendCellNotice says in words what a cell of the row stands for, taking the glyph and the tone
// from that cell, and saying nothing where the row draws nothing.
func appendCellNotice(notices []notice, cell view.Cell, text string) []notice {
	return appendNotice(notices, !cell.IsBlank() && text != "", view.Spell(cell.Glyph), cell.Tone, text)
}

func appendNotice(notices []notice, show bool, icon string, tone paint.Tone, text string) []notice { //nolint:revive // a params struct is the follow-up
	if !show {
		return notices
	}

	return append(notices, notice{
		icon: icon,
		tone: tone,
		text: text,
	})
}

// heldNoticeText is what the record has to say about an agent somebody is holding.
func heldNoticeText(item panel.AgentRow) string {
	if item.Agent.HeldAt == nil {
		return ""
	}

	if item.Alive {
		switch item.Agent.State {
		case panel.StateThinking:
			return heldWorkingNotice
		case panel.StateWaiting:
			return heldPromptedNotice
		case panel.StateUncertain:
			return heldUnreadableNotice
		case panel.StateIdle, panel.StateSettingUp,
			panel.StateFailedToCreate, panel.StateFailedToCleanup:
		}
	}

	return heldNotice
}

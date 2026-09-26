package ui

import (
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
	documentsNotice       = "it has written documents for you to read"
	heldNotice            = "paused: you stopped it and have its worktree"
	heldWorkingNotice     = "paused, but it has started working again"
	heldUnreadableNotice  = "paused, but its pane cannot be read, so it may be writing"
	heldPromptedNotice    = "paused, but stopped at a prompt, so a write lands if anybody answers"
)

const (
	localApprovedNotice            = "approved as it stands"
	localApprovedMovedNotice       = "approved, but the tree has changed since"
	localChangesOwedNotice         = "changes were asked for that the agent still owes"
	localChangesPushedNotice       = "ready for re-review, the agent has pushed the changes"
	localApprovedLandedNotice      = "approved and landed"
	localApprovedLandedMovedNotice = "approved and landed, with new work nobody has read"
	localChangesLandedNotice       = "changes asked for, but the tree landed already"
	localChangesLandedMovedNotice  = "changes asked for, the tree landed, plus there is unreviewed work"
	archivedApprovedNotice         = "approved while archived"
	archivedChangesOwedNotice      = "changes asked for + still owed when archived"
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

	// reviewSaid is what the review column says where it stands away from its baseline, which
	// is whose turn it is.
	reviewSaid = map[view.Review]string{
		view.ReviewApproved:            localApprovedNotice,
		view.ReviewApprovedMoved:       localApprovedMovedNotice,
		view.ReviewChangesRequested:    localChangesOwedNotice,
		view.ReviewChangesPushed:       localChangesPushedNotice,
		view.ReviewApprovedLanded:      localApprovedLandedNotice,
		view.ReviewApprovedLandedMoved: localApprovedLandedMovedNotice,
		view.ReviewChangesLanded:       localChangesLandedNotice,
		view.ReviewChangesLandedMoved:  localChangesLandedMovedNotice,
	}

	// archivedReviewSaid is what the review column says on an archived row.
	archivedReviewSaid = map[view.Review]string{
		view.ReviewApproved:         archivedApprovedNotice,
		view.ReviewChangesRequested: archivedChangesOwedNotice,
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
	notices = appendCellNotice(notices, view.DocumentCell(row), documentsNotice)

	if row.Archived {
		return appendCellNotice(notices, view.ReviewCell(row), archivedReviewSaid[row.Review])
	}

	return appendCellNotice(notices, view.ReviewCell(row), reviewSaid[row.Review])
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

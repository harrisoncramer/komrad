package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"
	"weave/internal/ui/paint"
	"weave/internal/view"
)

// notice is one of the marks a row draws said in words, carrying the part it plays rather than a
// colour, since the region it is drawn in decides that.
type notice struct {
	icon string
	tone paint.Tone
	text string
}

const (
	failedToCreateNotice  = "setup failed, it never launched"
	failedToCleanupNotice = "teardown failed, its directory is still there"
	failedRunNotice       = "a script weave fired failed, log unread"
	unreadMailNotice      = "mail waiting, it has not read it"
	worktreeRemovedNotice = "its directory no longer exists"
	sequenceNotice        = "a rebase is stopped part way in it"
	hiddenNotice          = "hidden from the sidebar"
	heldNotice            = "paused: you stopped it and have its worktree"
	heldWorkingNotice     = "paused, but it has started working again"
	heldUnreadableNotice  = "paused, but its pane cannot be read, so it may be writing"
	heldPromptedNotice    = "paused, but stopped at a prompt, so a write lands if anybody answers"
)

const (
	inLocalReviewNotice     = "in review, reviewer owed an answer"
	localApprovedNotice     = "local reviewer approved this round"
	localChangesAskedNotice = "local reviewer asked for changes"
)

// Explainer: a notice teaches one mark on the row above it, so it takes its glyph and its tone
// from the cell that row draws rather than naming either itself. Unviewed has no mark, and
// emphasisFor ranks the cursor above the brightening, so it never reaches the one row a record
// is ever drawn for, and a second's dwell on the row clears it, so it is not said either.

var (
	// failureSaid is what the status column drawn broken says about how an agent got there.
	failureSaid = map[agentstore.State]string{
		agentstore.StateFailedToCreate:  failedToCreateNotice,
		agentstore.StateFailedToCleanup: failedToCleanupNotice,
	}

	// repoSaid is what the repo column says where it stands away from its baseline.
	repoSaid = map[view.Repo]string{
		view.RepoGone:     worktreeRemovedNotice,
		view.RepoRebasing: sequenceNotice,
	}

	// reviewSaid is what the review column says, a released verdict and a reviewer still
	// reading sharing the one column.
	reviewSaid = map[view.Review]string{
		view.ReviewOpen:             inLocalReviewNotice,
		view.ReviewApproved:         localApprovedNotice,
		view.ReviewChangesRequested: localChangesAskedNotice,
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
	notices = appendCellNotice(notices, view.ReviewCell(row), reviewSaid[row.Review])

	return appendNotice(notices, item.localRounds > 0, "", paint.ToneMuted, localReviewRoundsNotice(item.localRounds))
}

// localReviewRoundsNotice counts against the agent holding the diff.
func localReviewRoundsNotice(rounds int32) string {
	if rounds == 1 {
		return "reviewed once"
	}

	return fmt.Sprintf("reviewed %d times", rounds)
}

// failureNotices is the whole of what an archived row has to say.
func failureNotices(r agentRow) []notice {
	row := snapshotRow(r, snapshotParams{})

	return appendCellNotice(nil, view.StatusCell(row), failureSaid[displayState(r.item)])
}

// appendCellNotice says in words what a cell of the row stands for, taking the glyph and the tone
// from that cell, and saying nothing where the row draws nothing.
func appendCellNotice(notices []notice, cell view.Cell, text string) []notice {
	return appendNotice(notices, !cell.IsBlank() && text != "", view.Spell(cell.Glyph), cell.Tone, text)
}

func appendNotice(notices []notice, show bool, icon string, tone paint.Tone, text string) []notice {
	if !show {
		return notices
	}

	return append(notices, notice{
		icon: icon,
		tone: tone,
		text: text,
	})
}

// Explainer: a hold is taken on one reading of a pane, and an agent that was about to start a
// turn reads exactly like one that has stopped. The reconciler goes on reading that pane every
// few seconds, so weave finds out, and what it does about it is say so rather than quietly let
// go of a tree somebody has an editor open on. A pane that has since gone unreadable is its own
// answer: it says nothing about whether the agent stopped, which is a different thing to tell
// somebody with an editor open than that it did.

// heldNoticeText is what the record has to say about an agent somebody is holding.
func heldNoticeText(item agentItem) string {
	if item.agent.HeldAt == nil {
		return ""
	}

	if item.alive {
		switch item.agent.State {
		case agentstore.StateThinking:
			return heldWorkingNotice
		case agentstore.StateWaiting:
			return heldPromptedNotice
		case agentstore.StateUncertain:
			return heldUnreadableNotice
		case agentstore.StateIdle, agentstore.StateSettingUp,
			agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		}
	}

	return heldNotice
}

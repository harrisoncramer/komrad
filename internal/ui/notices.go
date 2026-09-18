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

// Explainer: a notice is the record teaching one mark on the row above it, so every one of them
// takes its glyph and its tone from the cell that row draws rather than naming a glyph of its
// own. Two facts have no mark at all and are said without one: a row is hidden by being greyed
// and unviewed by being brightened, and the row the record is about is the row under the cursor,
// which is drawn in the cursor's own colour instead, so neither reaches the person reading it.
// Unviewed is left out all the same, because a second's dwell on the row clears it, and a line
// that comes and goes on its own is worse than no line. Hidden stays, and so does the count of
// review rounds, which the rebuilt row spends no column on at all.

// agentNotices orders the marks by how much each wants a person's attention.
func agentNotices(item agentItem) []notice {
	notices := appendCellNotice(nil, view.StatusCell(snapshotStatus(item), dormant(item)), failureNotice(displayState(item)))
	notices = appendCellNotice(notices, view.AlertCell(item.failedRun), failedRunNotice)
	notices = appendCellNotice(notices, view.MailCell(item.unreadMail), unreadMailNotice)

	repo := snapshotRepo(item.agent)
	notices = appendCellNotice(notices, view.RepoCell(repo), repoNotice(repo))
	notices = appendCellNotice(notices, view.HeldCell(item.agent.HeldAt != nil, heldWorking(item)), heldNoticeText(item))

	notices = appendNotice(notices, item.agent.Hidden, "", paint.ToneMuted, hiddenNotice)

	review := snapshotReview(item)
	notices = appendCellNotice(notices, view.ReviewCell(review), reviewNotice(review))

	return appendNotice(notices, item.localRounds > 0, "", paint.ToneMuted, localReviewRoundsNotice(item.localRounds))
}

// failureNotice is what the status column drawn broken says, the two failures being the only
// states that are a fact about the agent rather than about this second.
func failureNotice(state agentstore.State) string {
	switch state {
	case agentstore.StateFailedToCreate:
		return failedToCreateNotice
	case agentstore.StateFailedToCleanup:
		return failedToCleanupNotice
	case agentstore.StateIdle, agentstore.StateWaiting, agentstore.StateThinking,
		agentstore.StateUncertain, agentstore.StateSettingUp:
		return ""
	}

	return ""
}

// repoNotice is what the repo column drawn away from its baseline says, and nothing at all while
// the checkout is somewhere an agent can work.
func repoNotice(repo view.Repo) string {
	switch repo {
	case view.RepoGone:
		return worktreeRemovedNotice
	case view.RepoRebasing:
		return sequenceNotice
	case view.RepoClean:
		return ""
	}

	return ""
}

// reviewNotice is what the review column says, a released verdict and a reviewer still reading
// sharing the one column.
func reviewNotice(review view.Review) string {
	switch review {
	case view.ReviewOpen:
		return inLocalReviewNotice
	case view.ReviewApproved:
		return localApprovedNotice
	case view.ReviewChangesRequested:
		return localChangesAskedNotice
	case view.ReviewNone, view.ReviewCommented:
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

// failureNotices is the whole of what an archived row has to say.
func failureNotices(state agentstore.State) []notice {
	notices := appendNotice(nil, state == agentstore.StateFailedToCreate, stateIcon(state), stateTone(state), failedToCreateNotice)

	return appendNotice(notices, state == agentstore.StateFailedToCleanup, stateIcon(state), stateTone(state), failedToCleanupNotice)
}

// appendCellNotice says in words what a cell of the row stands for, taking the glyph and the tone
// from that cell, and saying nothing where the row draws nothing or the cell means nothing.
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

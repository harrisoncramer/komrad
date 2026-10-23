package agents

import (
	"fmt"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/view"
	agentv1 "komrad/proto/gen/agent/v1"
)

const (
	failedToCreateNotice  = "setup failed, it never launched"
	failedToCleanupNotice = "teardown failed, its directory is still there"
	turnFailedNotice      = "its last turn failed"
	failedRunNotice       = "a script komrad fired failed, log unread"
	unreadMailNotice      = "mail waiting, it has not read it"
	worktreeRemovedNotice = "its directory no longer exists"
	sequenceNotice        = "a rebase is stopped part way in it"
	hiddenNotice          = "hidden from the sidebar"
	documentsNotice       = "it has written documents for you to read"
	autoMergeNotice       = "merges on its own once checks and reviews pass"
)

var (
	// failureSaid is what the status column rendered broken says about how an agent got there.
	failureSaid = map[agentv1.AgentState]string{
		agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE:  failedToCreateNotice,
		agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP: failedToCleanupNotice,
	}

	// repoSaid is what the repo column says where it stands away from its baseline.
	repoSaid = map[view.Repo]string{
		view.RepoGone:     worktreeRemovedNotice,
		view.RepoRebasing: sequenceNotice,
	}
	forgeAlertSaid = map[view.Forge]string{
		view.ForgeFailing:     "has a failing check",
		view.ForgeConflicting: "no longer merges cleanly",
	}
	forgeReviewSaid = map[view.Forge]string{
		view.ForgeChangesRequested: "has changes requested",
		view.ForgeApproved:         "is approved and ready to merge",
	}
	forgeSaid = map[view.Forge]string{
		view.ForgeOpen:              "is open and waiting on a review",
		view.ForgeRereviewRequested: "is waiting on another review",
		view.ForgeChecksRunning:     "has checks running",
		view.ForgeApprovedWaiting:   "is approved but cannot merge yet",
		view.ForgeDraft:             "is still a draft",
		view.ForgeMerged:            "is merged",
		view.ForgeClosed:            "was closed without merging",
	}
)

// agentNotices orders the marks by how much each wants a person's attention, reading the row the
// list would render so a column the row leaves empty is a column the record says nothing about.
func agentNotices(glyphs glyph.Set, loaderFrame string, tasks panel.Tasks, r agentRow) []record.Notice {
	item, row := r.item, snapshotRow(r, snapshotParams{loaderFrame: loaderFrame, tasks: tasks})
	standing, failure := tasks.FindForAgent(item.Agent).PullRequest, describeFailure(item)
	if r.isTask() {
		standing = r.task.PullRequest
	}

	var notices []record.Notice
	if failure != "" {
		notices = append(notices, record.Notice{
			Icon: glyphs.Spell(glyph.MessageError),
			Tone: paint.ToneDanger,
			Text: failure,
		})
	}
	notices = appendCellNotice(notices, glyphs, view.AlertCell(row), failedRunNotice)
	notices = appendCellNotice(notices, glyphs, view.ForgeCell(row), standing.Describe(forgeAlertSaid[row.Forge]))
	notices = appendCellNotice(notices, glyphs, view.MailCell(row), unreadMailNotice)
	notices = appendCellNotice(notices, glyphs, view.ForgeCell(row), standing.Describe(forgeReviewSaid[row.Forge]))
	notices = appendCellNotice(notices, glyphs, view.RepoCell(row), repoSaid[row.Repo])
	notices = appendCellNotice(notices, glyphs, view.HiddenCell(row), hiddenNotice)
	notices = appendCellNotice(notices, glyphs, view.DocumentCell(row), documentsNotice)
	notices = appendCellNotice(notices, glyphs, view.AutoMergeCell(row), standing.Describe(autoMergeNotice))
	notices = appendCellNotice(notices, glyphs, view.ForgeCell(row), standing.Describe(forgeSaid[row.Forge]))

	return appendUnreadNotice(notices, glyphs, row)
}

// appendUnreadNotice says how many of the person's comments hold a reply they have not read.
func appendUnreadNotice(notices []record.Notice, glyphs glyph.Set, row view.Row) []record.Notice {
	if view.UnreadCell(row).IsBlank() {
		return notices
	}

	text := fmt.Sprintf("your comments with a reply you have not read: %d", row.PersonComments.Unread)

	return append(notices, record.Notice{
		Icon: glyphs.Spell(glyph.Unread),
		Tone: paint.ToneWarning,
		Text: text,
	})
}

// appendCellNotice says in words what a cell of the row stands for, taking the glyph and the tone
// from that cell, and saying nothing where the row renders nothing.
func appendCellNotice(notices []record.Notice, glyphs glyph.Set, cell view.Cell, text string) []record.Notice {
	if cell.IsBlank() || text == "" {
		return notices
	}

	return append(notices, record.Notice{
		Icon: view.Spell(glyphs, cell),
		Tone: cell.Tone,
		Text: text,
	})
}

// describeFailure says how a broken agent broke.
func describeFailure(item panel.AgentRow) string {
	state := item.DisplayState()
	if state != agentv1.AgentState_AGENT_STATE_ERRORED {
		return failureSaid[state]
	}

	if item.Agent.TurnError == "" {
		return turnFailedNotice
	}

	return turnFailedNotice + ": " + item.Agent.TurnError
}

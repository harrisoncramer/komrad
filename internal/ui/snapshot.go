package ui

import (
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/view"
)

// pullRequestKey is the metadata key an agent records the pull request it opened under.
const pullRequestKey = "pr"

// snapshotParams is what the panel already holds about the agents list, handed over as plain
// data so the reading below needs nothing else.
type snapshotParams struct {
	rows []agentRow
	// marks are the letters a person typed onto rows, keyed by the row key each names.
	marks map[string]string
	// selected are the agents taken into a selection several rows wide.
	selected selection
	// focused is the key of the row the caret stands on, empty when it stands on none.
	focused string
	// showHidden says the hidden rows are drawn alongside the rest, so none is stood in for.
	showHidden bool
	// nameWidth is the column names are cut to.
	nameWidth int
}

// agentsSnapshot reads the listing into the plain data the agents list draws itself from.
func agentsSnapshot(params snapshotParams) view.Snapshot {
	rows := make([]view.Row, 0, len(params.rows))
	for _, r := range params.rows {
		rows = append(rows, snapshotRow(r, params))
	}

	return view.Snapshot{
		Rows:      rows,
		Focused:   params.focused,
		NameWidth: params.nameWidth,
	}
}

// snapshotRow reads one row of the tree, a workflow header carrying only what a workflow can
// say for itself and the status of the orchestrator standing behind it.
func snapshotRow(r agentRow, params snapshotParams) view.Row {
	key := rowKey(r)
	row := view.Row{
		Key:         key,
		Mark:        params.marks[key],
		HiddenCount: standingIn(r.hiddenCount, params.showHidden),
	}

	if r.header {
		row.Kind = view.KindWorkflow
		row.Name = r.workflow.Name
		if r.orchestrator != nil {
			row.Status = snapshotStatus(*r.orchestrator)
		}

		return row
	}

	item := r.item
	row.Kind = snapshotKind(r)
	row.Name = item.agent.Name
	row.Status = snapshotStatus(item)
	row.Repo = snapshotRepo(item.agent)
	row.Review = snapshotReview(item)
	row.Forge = snapshotForge(item.agent)
	row.Selected = params.selected.has(item.agent.ID)
	row.Unviewed = item.unviewed
	row.Dim = item.agent.Hidden
	row.Held = item.agent.HeldAt != nil
	row.UnreadMail = item.unreadMail
	row.FailedRun = item.failedRun
	row.Scheduled = item.agent.ScheduleID != nil

	return row
}

// standingIn is how many rows one stands in for, which is none at all while they are all drawn.
func standingIn(count int, showHidden bool) int {
	if showHidden {
		return 0
	}

	return count
}

// snapshotKind is where an agent sits in the tree.
func snapshotKind(r agentRow) view.RowKind {
	if r.child {
		return view.KindChild
	}

	return view.KindAgent
}

// snapshotStatus is what an agent is doing this second, read off the state the row wears rather
// than the one the store holds, so an agent whose pane has gone reads as idle.
func snapshotStatus(item agentItem) view.Status {
	switch displayState(item) {
	case agentstore.StateThinking:
		return view.StatusThinking
	case agentstore.StateWaiting:
		return view.StatusWaiting
	case agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return view.StatusBroken
	case agentstore.StateIdle, agentstore.StateSettingUp, agentstore.StateUncertain:
		return view.StatusIdle
	}

	return view.StatusIdle
}

// snapshotRepo is where an agent's own checkout stands, a directory that has gone beating a
// rebase left part way in one that has not.
func snapshotRepo(agent agentstore.Agent) view.Repo {
	switch {
	case agent.WorktreeRemoved:
		return view.RepoGone
	case agent.SequenceInProgress:
		return view.RepoRebasing
	}

	return view.RepoClean
}

// snapshotReview is where the local review of an agent's work stands, a released verdict beating
// the reviewer still reading since one column carries both.
func snapshotReview(item agentItem) view.Review {
	switch reviewstore.Verdict(item.localVerdict) {
	case reviewstore.VerdictApproved:
		return view.ReviewApproved
	case reviewstore.VerdictChangesRequested:
		return view.ReviewChangesRequested
	case reviewstore.VerdictCommented:
	}

	if item.inLocalReview {
		return view.ReviewOpen
	}

	return view.ReviewNone
}

// snapshotForge is where the pull request opened from an agent's branch stands, which is only
// ever that one was opened, since nothing weave holds says what has become of it.
func snapshotForge(agent agentstore.Agent) view.Forge {
	for _, entry := range agent.Metadata {
		if entry.Key == pullRequestKey && entry.Value != "" {
			return view.ForgeOpen
		}
	}

	return view.ForgeNone
}

package ui

import (
	"regexp"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	"weave/internal/view"
)

// snapshotParams is what the panel already holds about the agents list, handed over as plain
// data so the reading below needs nothing else.
type snapshotParams struct {
	rows []agentRow
	// marks are the letters a person typed onto rows, keyed by the row key each names.
	marks map[string]string
	// selected are the agents taken into a selection several rows wide.
	selected selection
	// focused is the key of the row the cursor stands on, empty when it stands on none.
	focused string
	// caretHidden says the blink has the focus caret out of sight this frame.
	caretHidden bool
	// matcher is the search a person is filtering the list by, nil when they are not.
	matcher *regexp.Regexp
	// scheduled are the workflows at least one schedule launches into, keyed by workflow id.
	scheduled map[string]bool
	// showHidden says the hidden rows are drawn alongside the rest, so none is stood in for.
	showHidden bool
	// pane is how many columns the list is drawn in.
	pane int
	// dropped are the columns this screen does not draw at all.
	dropped view.Columns
}

// agentsSnapshot reads the listing into the plain data the agents list draws itself from.
func agentsSnapshot(params snapshotParams) view.Snapshot {
	rows := make([]view.Row, 0, len(params.rows))
	for _, r := range params.rows {
		rows = append(rows, snapshotRow(r, params))
	}

	return view.Snapshot{
		Rows:        rows,
		CaretHidden: params.caretHidden,
		Focused:     params.focused,
		Pane:        params.pane,
		Dropped:     params.dropped,
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
		row.Matches = matchesIn(params.matcher, r.workflow.Name)
		row.Scheduled = params.scheduled[r.workflow.ID]
		row.Dim = r.workflow.ArchivedAt != nil

		// A workflow's bar is its orchestrator's, and one that has never had an orchestrator has
		// no more of a process behind it than a dormant one.
		if r.orchestrator == nil {
			row.Dormant = true

			return row
		}

		row.Status = snapshotStatus(*r.orchestrator)
		row.Dormant = dormant(*r.orchestrator)

		return row
	}

	item := r.item
	row.Kind = snapshotKind(r)
	row.Name = item.agent.Name
	row.Matches = matchesIn(params.matcher, item.agent.Name)
	row.Status = snapshotStatus(item)
	row.Dormant = dormant(item)
	row.Repo = snapshotRepo(item.agent)
	row.Review = snapshotReview(item)
	row.Selected = params.selected.has(item.agent.ID)
	row.Unviewed = item.unviewed
	row.Dim = item.agent.Hidden || item.agent.ArchivedAt != nil
	row.Held = item.agent.HeldAt != nil
	row.HeldWorking = row.Held && heldWorking(item)
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
	// A pause is something a person did rather than something the agent is doing, so it wants
	// that person however the agent was left.
	if item.agent.HeldAt != nil {
		return view.StatusWaiting
	}

	switch displayState(item) {
	case agentstore.StateThinking, agentstore.StateSettingUp:
		return view.StatusThinking
	case agentstore.StateWaiting, agentstore.StateUncertain:
		return view.StatusWaiting
	case agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return view.StatusBroken
	case agentstore.StateIdle:
		return view.StatusIdle
	}

	return view.StatusIdle
}

// matchesIn is the stretches of a name a search picked out, in bytes of that name.
func matchesIn(matcher *regexp.Regexp, name string) []view.Span {
	if matcher == nil {
		return nil
	}

	found := matcher.FindAllStringIndex(name, -1)
	if len(found) == 0 {
		return nil
	}

	spans := make([]view.Span, 0, len(found))
	for _, at := range found {
		spans = append(spans, view.Span{Start: at[0], End: at[1]})
	}

	return spans
}

// heldWorking reports whether an agent somebody paused is working anyway, which weave finds out
// by going on reading the pane rather than by the hold failing.
func heldWorking(item agentItem) bool {
	if !item.alive {
		return false
	}

	switch item.agent.State {
	case agentstore.StateThinking, agentstore.StateWaiting:
		return true
	case agentstore.StateIdle, agentstore.StateUncertain, agentstore.StateSettingUp,
		agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
		return false
	}

	return false
}

// dormant reports whether nothing is running behind a row, which the state it wears cannot say
// since an agent whose pane has gone wears the same idle as one sitting quiet.
func dormant(item agentItem) bool {
	return !item.alive && item.agent.State.DoesPaneDecide()
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
		if !item.inLocalReview {
			return view.ReviewCommented
		}
	}

	if item.inLocalReview {
		return view.ReviewOpen
	}

	return view.ReviewNone
}

package ui

import (
	"regexp"

	"komrad/internal/git"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
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
	// caret is how the arrow on the focused row stands this frame.
	caret view.Caret
	// barBlinkedOut says the blink has the working bars in their thin half this frame.
	barBlinkedOut bool
	// matcher is the search a person is filtering the list by, nil when they are not.
	matcher *regexp.Regexp
	// scheduled are the workflows at least one schedule launches into, keyed by workflow id.
	scheduled map[string]bool
	// showHidden says the hidden rows are drawn alongside the rest, so none is stood in for.
	showHidden bool
	// flat says no workflow header is drawn over the rows.
	flat bool
	// pane is how many columns the list is drawn in.
	pane int
}

// agentsSnapshot reads the listing into the plain data the agents list draws itself from.
func agentsSnapshot(params snapshotParams) view.Snapshot {
	rows := xslice.Map(params.rows, func(r agentRow) view.Row {
		return snapshotRow(r, params)
	})

	return view.Snapshot{
		Rows:          rows,
		Caret:         params.caret,
		BarBlinkedOut: params.barBlinkedOut,
		Focused:       params.focused,
		Pane:          params.pane,
	}
}

// snapshotRow reads one row of the tree, a workflow header carrying only what a workflow can
// say for itself and the status of the orchestrator standing behind it.
func snapshotRow(r agentRow, params snapshotParams) view.Row {
	key := rowKey(r)
	row := view.Row{
		Key:         key,
		Mark:        params.marks[rowID(r)],
		HiddenCount: standingIn(r.hiddenCount, params.showHidden),
	}

	if r.header {
		row.Kind = view.KindWorkflow
		row.Name = r.workflow.Name
		row.Matches = matchesIn(params.matcher, r.workflow.Name)
		row.Scheduled = params.scheduled[r.workflow.ID]
		row.Archived = r.workflow.ArchivedAt != nil

		// A workflow's bar is its orchestrator's, and one with no orchestrator running behind it
		// draws no bar at all.
		if r.orchestrator == nil || snapshotProcess(*r.orchestrator) == view.ProcessStopped {
			row.Process = view.ProcessNone

			return row
		}

		row.Status = snapshotStatus(*r.orchestrator)
		row.Process = view.ProcessAlive

		return row
	}

	item := r.item
	row = agentFacts(row, item)
	row.Kind = snapshotKind(r, params.flat)
	row.Name = item.Agent.Name
	row.Matches = matchesIn(params.matcher, item.Agent.Name)
	row.Repo = snapshotRepo(item.Agent)
	row.Review = snapshotReview(item)
	row.Forge = snapshotForge(item)
	row.Selected = params.selected.has(key)

	return row
}

// agentFacts is everything a row says about the agent standing behind it.
func agentFacts(row view.Row, item panel.AgentRow) view.Row {
	row.Status = snapshotStatus(item)
	row.Process = snapshotProcess(item)
	row.Archived = item.Agent.ArchivedAt != nil
	row.Hidden = item.Agent.Hidden && !row.Archived
	row.Held = item.Agent.HeldAt != nil
	row.HeldWorking = row.Held && isHeldWorking(item)
	row.UnreadMail = item.UnreadMail
	row.FailedRun = item.FailedRun
	row.Scheduled = item.Agent.ScheduleID != nil

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
func snapshotKind(r agentRow, flat bool) view.RowKind {
	switch {
	case r.child:
		return view.KindChild
	case flat:
		return view.KindResult
	}

	return view.KindAgent
}

// snapshotStatus is what an agent is doing this second, read off the state the row wears rather
// than the one the store holds, so an agent whose pane has gone reads as idle.
func snapshotStatus(item panel.AgentRow) view.Status {
	// A pause is something a person did rather than something the agent is doing, so it wants
	// that person however the agent was left.
	if item.Agent.HeldAt != nil {
		return view.StatusWaiting
	}

	switch displayState(item) {
	case panel.StateThinking, panel.StateSettingUp:
		return view.StatusThinking
	case panel.StateWaiting, panel.StateUncertain:
		return view.StatusWaiting
	case panel.StateFailedToCreate, panel.StateFailedToCleanup:
		return view.StatusBroken
	case panel.StateIdle:
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

	spans := xslice.Map(found, func(at []int) view.Span {
		return view.Span{Start: at[0], End: at[1]}
	})

	return spans
}

// isHeldWorking reports whether an agent somebody paused is working anyway.
func isHeldWorking(item panel.AgentRow) bool {
	if !item.Alive {
		return false
	}

	switch item.Agent.State {
	case panel.StateThinking, panel.StateWaiting:
		return true
	case panel.StateIdle, panel.StateUncertain, panel.StateSettingUp,
		panel.StateFailedToCreate, panel.StateFailedToCleanup:
		return false
	}

	return false
}

// snapshotProcess reports whether anything is running behind a row, which the state it wears
// cannot say since an agent whose pane has gone wears the same idle as one sitting quiet.
func snapshotProcess(item panel.AgentRow) view.Process {
	if !item.Alive && item.Agent.State.DoesPaneDecide() {
		return view.ProcessStopped
	}

	return view.ProcessAlive
}

// snapshotRepo is where an agent's own checkout stands, a directory that has gone beating a
// rebase left part way in one that has not.
func snapshotRepo(agent panel.Agent) view.Repo {
	switch {
	case agent.WorktreeRemoved:
		return view.RepoGone
	case agent.SequenceInProgress:
		return view.RepoRebasing
	}

	return view.RepoClean
}

// snapshotReview is where the tree on disk stands with the local review of an agent's work, the
// last conclusion released and whether the worktree still matches the round it was given on.
func snapshotReview(item panel.AgentRow) view.Review {
	tree := item.LocalTree
	if item.Agent.ArchivedAt != nil {
		tree = git.StandingSame
	}

	switch item.LocalVerdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		switch tree {
		case git.StandingMoved:
			return view.ReviewApprovedMoved
		case git.StandingLanded:
			return view.ReviewApprovedLanded
		case git.StandingLandedMoved:
			return view.ReviewApprovedLandedMoved
		case git.StandingSame:
		}

		return view.ReviewApproved
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		switch tree {
		case git.StandingMoved:
			return view.ReviewChangesPushed
		case git.StandingLanded:
			return view.ReviewChangesLanded
		case git.StandingLandedMoved:
			return view.ReviewChangesLandedMoved
		case git.StandingSame:
		}

		return view.ReviewChangesRequested
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return view.ReviewNone
}

// snapshotForge is where the pull request opened from an agent's branch stands, as the forge
// column draws it.
func snapshotForge(item panel.AgentRow) view.Forge {
	switch item.PullRequest.State {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE:
		return view.ForgeNone
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN:
		return view.ForgeOpen
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED:
		return view.ForgeApproved
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING:
		return view.ForgeApprovedWaiting
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED:
		return view.ForgeChangesRequested
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT:
		return view.ForgeDraft
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED:
		return view.ForgeMerged
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
		return view.ForgeClosed
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING:
		return view.ForgeFailing
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING:
		return view.ForgeConflicting
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD:
	}

	return view.ForgeUnread
}

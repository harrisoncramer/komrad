package ui

import (
	"komrad/internal/ui/paint"
	"path/filepath"
	"regexp"
	"slices"

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
	// triage is the zone rendered ahead of the rows, empty on a list that renders none.
	triage triageSnapshot
	// selected are the agents taken into a selection several rows wide.
	selected selection
	// wholeTasks are the ids of the tasks a selection takes whole, drawn as selected themselves.
	wholeTasks []string
	// focused is the key of the row the cursor stands on, empty when it stands on none.
	focused string
	// caret is how the arrow on the focused row stands this frame.
	caret       view.Caret
	loaderFrame string
	// matcher is the search a person is filtering the list by, nil when they are not.
	matcher *regexp.Regexp
	// picked are the keys of the rows the search's qualifiers pick, nil when it names none.
	picked map[string]bool
	// tasks are the tasks the agents work on, keyed by id.
	tasks taskIndex
	// scheduled are the workflows at least one schedule launches into, keyed by workflow id.
	scheduled map[string]bool
	// workflowDirs is the directory each workflow launches its agents in, keyed by workflow id.
	workflowDirs map[string]string
	// showHidden says the hidden rows are rendered alongside the rest, so none is stood in for.
	showHidden bool
	// flat says no workflow header is rendered over the rows.
	flat bool
	// pane is how many columns the list is rendered in.
	pane int
	// renaming is the name being typed over one row, nil when none is.
	renaming *nameEdit
}

// agentsSnapshot reads the listing into the plain data the agents list renders itself from.
func agentsSnapshot(params snapshotParams) view.Snapshot {
	rows := triageRows(params)
	trees := treesOf(params.rows, params.flat)
	for i, r := range params.rows {
		row := params.renaming.renderedOver(snapshotRow(r, params))
		row.Tree = trees[i]
		rows = append(rows, row)
	}

	return view.Snapshot{
		Rows:    rows,
		Caret:   params.caret,
		Focused: params.focused,
		Pane:    params.pane,
	}
}

// snapshotRow reads one row of the tree, a workflow header carrying only what a workflow can
// say for itself and the status of the orchestrator standing behind it.
func snapshotRow(r agentRow, params snapshotParams) view.Row {
	key := rowKey(r)
	row := view.Row{
		Key:         key,
		HiddenCount: standingIn(r.hiddenCount, params.showHidden),
	}

	if r.isTask() {
		row.Process = view.ProcessNone
		if writer, ok := r.writer(); ok {
			row = agentFacts(row, writer)
		}
		row.Kind = view.KindTask
		row.Name = r.task.Slug
		row.Matches = params.matchesOf(r, r.task.Slug)
		row.Archived = r.task.ArchivedAt != nil
		row.Hidden = r.task.Hidden && !row.Archived
		row.FailedRun = row.FailedRun || r.task.FailedRun
		row.Documents = r.task.Documents > 0
		row.Repo = snapshotRepo(r.task.WorktreeRemoved, r.task.Sequence)
		row.InWorkflowDir = isInWorkflowDir(r.task.WorkflowID, r.task.Cwd, params.workflowDirs)
		row.ReviewerComments, row.PersonComments = convertComments(r.task.ReviewerComments), convertComments(r.task.PersonComments)
		row.Forge = snapshotForge(r.task.PullRequest)
		row.AutoMerge = r.task.PullRequest.MergeReadiness.AutoMerge
		row.LoaderFrame = params.loaderFrame
		row.Selected = slices.Contains(params.wholeTasks, r.task.ID)
		row.Tail = standTail(r, params.tasks)

		return row
	}

	if r.header {
		row.Kind = view.KindWorkflow
		row.Name = r.workflow.Name
		row.Matches = params.matchesOf(r, r.workflow.Name)
		row.Scheduled = params.scheduled[r.workflow.ID]
		row.Archived = r.workflow.ArchivedAt != nil
		row.UnreadMail = r.orchestrator != nil && r.orchestrator.UnreadMail
		row.Documents = r.orchestrator != nil && r.orchestrator.Documents > 0

		// A workflow's bar is its orchestrator's, and one with no orchestrator running behind it
		// renders no bar at all.
		if r.orchestrator == nil || snapshotProcess(*r.orchestrator) == view.ProcessStopped {
			row.Process = view.ProcessNone

			return row
		}

		row.Status = snapshotStatus(*r.orchestrator)
		row.Process = view.ProcessAlive
		row.Tool = r.orchestrator.Agent.Tool
		row.LoaderFrame = params.loaderFrame

		return row
	}

	item := r.item
	task := params.tasks.findForAgent(item.Agent)
	row = agentFacts(row, item)
	row.Documents = row.Documents && !r.underTask
	row.Kind = snapshotKind(r, params.flat)
	row.Nested = r.underTask
	row.Name = item.Agent.Name
	row.Matches = params.matchesOf(r, item.Agent.Name)
	row.Repo = snapshotRepo(task.WorktreeRemoved, task.Sequence)
	row.InWorkflowDir = isInWorkflowDir(item.Agent.WorkflowID, item.Agent.Cwd, params.workflowDirs)
	if item.Agent.Role != panel.RoleReviewer {
		row.ReviewerComments, row.PersonComments = convertComments(task.ReviewerComments), convertComments(task.PersonComments)
	}
	row.Forge = snapshotForge(task.PullRequest)
	row.AutoMerge = task.PullRequest.MergeReadiness.AutoMerge
	row.LoaderFrame = params.loaderFrame
	row.Selected = params.selected.has(toTreeRowKey(key))

	return row
}

// agentFacts is everything a row says about the agent standing behind it.
func agentFacts(row view.Row, item panel.AgentRow) view.Row {
	row.Status = snapshotStatus(item)
	row.Process = snapshotProcess(item)
	row.Archived = item.Agent.ArchivedAt != nil
	row.Held = item.Agent.HeldAt != nil
	row.HeldWorking = row.Held && isHeldWorking(item)
	row.UnreadMail = item.UnreadMail
	row.FailedRun = item.FailedRun
	row.Documents = item.Documents > 0
	row.Scheduled = item.Agent.ScheduleID != nil
	row.Tool = item.Agent.Tool

	return row
}

// treesOf is where each row hangs in the tree drawn down the list, read off the rows as they are
// rendered so a row the search left out leaves no gap in it, and none at all on a flat list.
func treesOf(rows []agentRow, flat bool) []*view.Tree {
	trees := make([]*view.Tree, len(rows))
	if flat {
		return trees
	}

	depths := xslice.Map(rows, treeDepth)
	var above []bool
	for i, depth := range depths {
		if depth < 0 {
			continue
		}

		last := true
		for _, next := range depths[i+1:] {
			if next <= depth {
				last = next < depth
				break
			}
		}

		for len(above) < depth {
			above = append(above, true)
		}

		trees[i] = &view.Tree{Last: last}
		if depth > 0 {
			trees[i].Above = slices.Clone(above[:depth])
		}
		above = append(above[:depth], last)
	}

	return trees
}

// treeDepth is how many rows one hangs under below its workflow, and -1 for the workflow itself.
func treeDepth(r agentRow) int {
	switch {
	case r.isTask():
		return r.depth
	case r.header:
		return -1
	case r.underTask:
		return r.depth + 1
	}

	return 0
}

// standingIn is how many rows one stands in for, which is none at all while they are all rendered.
func standingIn(count int, showHidden bool) int {
	if showHidden {
		return 0
	}

	return count
}

// snapshotKind is where an agent sits in the tree, a reviewer set in under its task's row on a flat
// list too.
func snapshotKind(r agentRow, flat bool) view.RowKind {
	if flat && !r.underTask {
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
	case panel.StateFailedToCreate, panel.StateFailedToCleanup, panel.StateErrored:
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
	case panel.StateIdle, panel.StateErrored, panel.StateUncertain, panel.StateSettingUp,
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

// snapshotRepo is where a checkout stands, a directory that has gone beating a rebase left part
// way in one that has not.
func snapshotRepo(removed bool, sequence panel.Sequence) view.Repo {
	switch {
	case removed:
		return view.RepoGone
	case sequence.IsInProgress():
		return view.RepoRebasing
	}

	return view.RepoClean
}

// isInWorkflowDir reports whether a directory is its workflow's own rather than a worktree.
func isInWorkflowDir(workflowID, cwd string, dirs map[string]string) bool {
	dir, ok := dirs[workflowID]

	return ok && cwd != "" && filepath.Clean(cwd) == filepath.Clean(dir)
}

func convertComments(counts *agentv1.CommentCounts) *view.Comments {
	if counts == nil {
		return nil
	}

	return &view.Comments{
		Open:     int(counts.GetOpen()),
		Resolved: int(counts.GetResolved()),
		Unread:   int(counts.GetUnread()),
	}
}

// snapshotForge is where the pull request opened from a branch stands, as the forge
// column renders it.
func snapshotForge(standing panel.PullRequestStanding) view.Forge {
	switch standing.State {
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
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED:
		return view.ForgeRereviewRequested
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING:
		return view.ForgeChecksRunning
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

// matchesOf is the stretches of a row's name the search lights, the whole of it when only qualifiers
// picked the row.
func (p snapshotParams) matchesOf(r agentRow, name string) []view.Span {
	return pickedMatches(p.matcher, p.picked, r.key(), name)
}

// pickedMatches is the stretches of a row's name the search lights, the whole of it when only
// qualifiers picked the row.
func pickedMatches(matcher *regexp.Regexp, picked map[string]bool, key, name string) []view.Span {
	if picked == nil {
		return matchesIn(matcher, name)
	}
	if !picked[key] {
		return nil
	}
	if matcher == nil {
		return []view.Span{{Start: 0, End: len(name)}}
	}

	return matchesIn(matcher, name)
}

// standTail is what a task's row says after its name about the task it stands on.
func standTail(r agentRow, tasks taskIndex) view.Cell {
	stand := r.task.Stand
	if stand == nil {
		return view.Cell{}
	}

	parent := tasks[stand.GetTaskId()].Slug
	switch stand.GetBlocked() {
	case "conflicts":
		return view.Cell{Text: "behind " + parent + ", conflicts", Tone: paint.ToneWarning}
	case "lost":
		return view.Cell{Text: "lost its place on " + parent, Tone: paint.ToneWarning}
	case "closed":
		return view.Cell{Text: "on " + parent + ", closed unmerged", Tone: paint.ToneWarning}
	}

	return view.Cell{Text: "on " + parent, Tone: paint.ToneMuted}
}

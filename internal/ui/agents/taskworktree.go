package agents

import (
	"fmt"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
)

// taskWorktree is what has been read of a task's directory, and nothing for one that is not a
// worktree komrad holds.
func taskWorktree(inspections map[string]panel.WorktreeInspection, cwd string) (panel.WorktreeInspection, bool) {
	read, ok := inspections[cwd]
	if !ok || !read.IsAnswered || read.IsSettled() {
		return panel.WorktreeInspection{}, false
	}

	return read, true
}

// branchFieldKey labels the branch a task's checkout is on.
const branchFieldKey = "branch"

// checkoutFieldsParams is a task and what was read of its directory.
type checkoutFieldsParams struct {
	task panel.Task
	// worktree is what the server has answered about the task's directory, and read says
	// whether it has answered that the directory is a worktree at all.
	worktree panel.WorktreeInspection
	read     bool
	now      time.Time
}

// checkoutFields are the record's rows for a task's checkout: its branch and the pull requests
// from it, then where its directory stands.
func checkoutFields(params checkoutFieldsParams) []record.Field {
	var fields []record.Field
	if params.task.Branch != nil {
		fields = append(fields, record.Field{Label: branchFieldKey, Value: *params.task.Branch})
		fields = append(fields, pullRequestFields(params.task.PullRequest, params.task.PullRequests)...)
	}
	if params.read {
		fields = append(fields, worktreeStandingFields(params.worktree, params.now)...)
	}

	return fields
}

// buildCheckoutParams is a task's checkout as the listing and the reads hold it.
func buildCheckoutParams(inspections map[string]panel.WorktreeInspection, now time.Time, task panel.Task) checkoutFieldsParams {
	worktree, read := taskWorktree(inspections, task.Cwd)

	return checkoutFieldsParams{
		task:     task,
		worktree: worktree,
		read:     read,
		now:      now,
	}
}

// worktreeStandingFields are the record's rows for where a task's directory stands: what is
// uncommitted in it and what it takes on disk, with the refusal above them when the last read
// failed.
func worktreeStandingFields(read panel.WorktreeInspection, now time.Time) []record.Field {
	fields := []record.Field{}
	if read.Err != nil {
		fields = append(fields, record.Field{Label: "worktree", Value: "could not read: " + errui.GetReason(read.Err)})
	}
	if !read.IsRead() {
		return fields
	}

	if read.Worktree.Readable {
		fields = append(fields, record.Field{Label: "changes", Value: read.Changes()})
		if read.LandedCommits > 0 {
			fields = append(fields, record.Field{Label: "landed", Value: fmt.Sprintf("%s the base already holds", label.Count(read.LandedCommits, "commit", "commits"))})
		}
		if read.IsSequencing {
			fields = append(fields, record.Field{Label: "sequence", Value: "a merge or rebase is part-way through"})
		}
	}

	return append(fields, record.Field{Label: "size", Value: read.Worktree.SizeAt(now)})
}

// recordAgent is the agent the row's record is about, which for a task is its writer, and none on
// a workflow.
func (r agentRow) recordAgent(l panel.Listing) (panel.AgentRow, bool) {
	switch {
	case r.isTask():
		return l.FindWriterInCheckout(*r.task)
	case r.header:
		return panel.AgentRow{}, false
	}

	return r.item, true
}

// recordTask is the task whose checkout the row's record shows, and none on a row with none.
func (r agentRow) recordTask(l panel.Listing) (panel.Task, bool) {
	switch {
	case r.isTask():
		return *r.task, true
	case r.header || r.underTask:
		return panel.Task{}, false
	}

	return l.Tasks.FindForAgent(r.item.Agent), true
}

package ui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/logging"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	loopv1 "komrad/proto/gen/loop/v1"
	taskv1 "komrad/proto/gen/task/v1"

	tea "charm.land/bubbletea/v2"
)

// worktreeReadTTL is how long a reading of a task's directory stands before the cursor resting
// on the row asks for it again.
const worktreeReadTTL = time.Minute

// inspectTaskWorktreeCmd asks for the hovered task's directory where nothing stands for it or
// what stands is stale.
func (m *model) inspectTaskWorktreeCmd(now time.Time) tea.Cmd {
	task, ok := m.hoveredRecordTask()
	if !ok || task.Cwd == "" {
		return nil
	}
	if read, asked := m.inspections[task.Cwd]; asked && !read.isStale(now) {
		return nil
	}

	return m.readWorktreeCmd(task.Cwd)
}

// wakeCwdSyncCmd wakes the cwd sync while the cursor rests on a live task or agent.
func (m *model) wakeCwdSyncCmd() tea.Cmd {
	if !m.isHoveringLiveCheckout() {
		return nil
	}
	clients := m.api

	return func() tea.Msg {
		if err := clients.wakeLoop(loopv1.Loop_LOOP_CWD_SYNC); err != nil {
			ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
			defer cancel()

			logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to wake the cwd sync", logging.Err(err)...)
		}

		return nil
	}
}

// isHoveringLiveCheckout reports whether the record is about a live task or agent with a directory.
func (m *model) isHoveringLiveCheckout() bool {
	if task, ok := m.hoveredRecordTask(); ok && task.Cwd != "" && task.ArchivedAt == nil {
		return true
	}
	item, ok := m.hoveredRecordAgent()

	return ok && item.Agent.Cwd != "" && item.Agent.ArchivedAt == nil
}

// rereadTaskWorktreeCmd asks for the hovered task's directory again however fresh what stands
// for it is, short of an answer that will never change.
func (m *model) rereadTaskWorktreeCmd() tea.Cmd {
	task, ok := m.hoveredRecordTask()
	if !ok || task.Cwd == "" || m.inspections[task.Cwd].isSettled() {
		return nil
	}

	return m.readWorktreeCmd(task.Cwd)
}

// taskWorktree is what has been read of a task's directory, and nothing for one that is not a
// worktree komrad holds.
func taskWorktree(inspections map[string]worktreeInspection, cwd string) (worktreeInspection, bool) {
	read, ok := inspections[cwd]
	if !ok || !read.answered || read.isSettled() {
		return worktreeInspection{}, false
	}

	return read, true
}

// branchFieldKey labels the branch a task's checkout is on.
const branchFieldKey = "branch"

// checkoutFieldsParams is a task and what was read of its directory.
type checkoutFieldsParams struct {
	task panel.Task
	// worktree is what the service has answered about the task's directory, and read says
	// whether it has answered that the directory is a worktree at all.
	worktree worktreeInspection
	read     bool
	now      time.Time
}

// checkoutFields are the record's rows for a task's checkout: its branch and the pull requests
// from it, then where its directory stands.
func checkoutFields(params checkoutFieldsParams) []recordField {
	var fields []recordField
	if params.task.Branch != nil {
		fields = append(fields, recordField{label: branchFieldKey, value: *params.task.Branch})
		fields = append(fields, pullRequestFields(params.task.PullRequest, params.task.PullRequests)...)
	}
	if params.read {
		fields = append(fields, worktreeStandingFields(params.worktree, params.now)...)
	}

	return fields
}

// buildCheckoutParams is a task's checkout as the listing and the reads hold it.
func (m *model) buildCheckoutParams(task panel.Task) checkoutFieldsParams {
	worktree, read := taskWorktree(m.inspections, task.Cwd)

	return checkoutFieldsParams{
		task:     task,
		worktree: worktree,
		read:     read,
		now:      time.Now(),
	}
}

// worktreeStandingFields are the record's rows for where a task's directory stands: what is
// uncommitted in it and what it takes on disk, with the refusal above them when the last read
// failed.
func worktreeStandingFields(read worktreeInspection, now time.Time) []recordField {
	fields := []recordField{}
	if read.err != nil {
		fields = append(fields, recordField{label: "worktree", value: "could not read: " + errui.GetReason(read.err)})
	}
	if !read.isRead() {
		return fields
	}

	if read.item.readable {
		fields = append(fields, recordField{label: "changes", value: changesField(read)})
		if read.landed > 0 {
			fields = append(fields, recordField{label: "landed", value: fmt.Sprintf("%s the base already holds", label.Count(read.landed, "commit", "commits"))})
		}
		if read.sequencing {
			fields = append(fields, recordField{label: "sequence", value: "a merge or rebase is part-way through"})
		}
	}

	return append(fields, recordField{label: "size", value: sizeField(read.item, now)})
}

// taskDetailsTags is what the record's rule carries for a task.
func taskDetailsTags(m *model, task panel.Task) []string {
	worktree, _ := taskWorktree(m.inspections, task.Cwd)

	return append(buildWorktreeStateTags(worktree), buildDiffStatTag(task.DiffStat))
}

// buildWorktreeStateTags is what is uncommitted, unpushed and unpulled in the directory.
func buildWorktreeStateTags(read worktreeInspection) []string {
	if !read.isRead() || !read.item.readable {
		return nil
	}

	warning := paint.Panel.Style(paint.ToneWarning)
	var parts []string
	if n := len(read.modified); n > 0 {
		parts = append(parts, warning.Render(label.Count(n, "modified", "modified")))
	}
	if n := len(read.untracked); n > 0 {
		parts = append(parts, warning.Render(label.Count(n, "untracked", "untracked")))
	}
	switch {
	case read.item.branch == "":
	case !read.pushed && (read.baseRef == "" || read.ahead > 0):
		parts = append(parts, warning.Render("not pushed"))
	case read.pushed && read.unpushed > 0 && (read.ahead > 0 || !read.unpulledLanded):
		parts = append(parts, warning.Render(fmt.Sprintf("%s%d unpushed", glyph.Up, read.unpushed)))
	}
	if read.pushed && read.unpulled > 0 && !read.unpulledLanded {
		parts = append(parts, warning.Render(fmt.Sprintf("%s%d unpulled", glyph.Down, read.unpulled)))
	}

	return parts
}

// buildDiffStatTag is the lines the branch adds and removes since its base, and nothing where no
// base could be measured against.
func buildDiffStatTag(stat *taskv1.DiffStat) string {
	if stat == nil {
		return ""
	}
	if stat.GetAdded() == 0 && stat.GetRemoved() == 0 {
		return dimStyle().Render("clean")
	}

	return paint.Panel.Style(paint.ToneSuccess).Render(fmt.Sprintf("+%d", stat.GetAdded())) + " " + paint.Panel.Style(paint.ToneDanger).Render(fmt.Sprintf("−%d", stat.GetRemoved()))
}

// hoveredRecordAgent is the agent the record under the agents list or the archive is about, which
// for a task is its writer, and none on any other screen or row.
func (m *model) hoveredRecordAgent() (panel.AgentRow, bool) {
	screen, ok := m.active().(hoveredAgentReporter)
	if !ok {
		return panel.AgentRow{}, false
	}

	row, ok := screen.hoveredRow(m)
	if !ok {
		return panel.AgentRow{}, false
	}
	if row.isTask() {
		return m.listing.findCheckoutWriter(*row.task)
	}
	if row.header {
		return panel.AgentRow{}, false
	}

	return row.item, true
}

// hoveredRecordTask is the task whose checkout the record under the agents list or the archive
// shows, and none on any other screen or row.
func (m *model) hoveredRecordTask() (panel.Task, bool) {
	screen, ok := m.active().(hoveredAgentReporter)
	if !ok {
		return panel.Task{}, false
	}

	row, ok := screen.hoveredRow(m)
	switch {
	case !ok:
		return panel.Task{}, false
	case row.isTask():
		return *row.task, true
	case row.header || row.underTask:
		return panel.Task{}, false
	}

	return m.listing.findTask(row.item.Agent), true
}

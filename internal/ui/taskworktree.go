package ui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/errui"
	"komrad/internal/logging"
	"komrad/internal/ui/api"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	loopv1 "komrad/proto/gen/loop/v1"

	tea "charm.land/bubbletea/v2"
)

// worktreeReadTTL is how long a reading of a task's directory stands before the cursor resting
// on the row asks for it again.
const worktreeReadTTL = time.Minute

// isStale reports whether the answer is old enough to ask for again.
func isStale(read panel.WorktreeInspection, now time.Time) bool {
	return !read.Answered || (!read.IsSettled() && now.Sub(read.ReadAt) > worktreeReadTTL)
}

// inspectTaskWorktree asks for the hovered task's directory where nothing stands for it or
// what stands is stale.
func (m *model) inspectTaskWorktree(now time.Time) screen.Request {
	task, ok := m.hoveredRecordTask()
	if !ok || task.Cwd == "" {
		return nil
	}
	if read, asked := m.inspections[task.Cwd]; asked && !isStale(read, now) {
		return nil
	}

	return screen.Inspect{Dir: task.Cwd}
}

// wakeCwdSyncCmd wakes the cwd sync while the cursor rests on a live task or agent.
func (m *model) wakeCwdSyncCmd() tea.Cmd {
	if !m.isHoveringLiveCheckout() {
		return nil
	}
	clients := m.api

	return func() tea.Msg {
		if err := clients.WakeLoop(loopv1.Loop_LOOP_CWD_SYNC); err != nil {
			ctx, cancel := context.WithTimeout(context.Background(), api.RequestTimeout)
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

// rereadTaskWorktree asks for the hovered task's directory again however fresh what stands
// for it is, short of an answer that will never change.
func (m *model) rereadTaskWorktree() screen.Request {
	task, ok := m.hoveredRecordTask()
	if !ok || task.Cwd == "" || m.inspections[task.Cwd].IsSettled() {
		return nil
	}

	return screen.Inspect{Dir: task.Cwd}
}

// taskWorktree is what has been read of a task's directory, and nothing for one that is not a
// worktree komrad holds.
func taskWorktree(inspections map[string]panel.WorktreeInspection, cwd string) (panel.WorktreeInspection, bool) {
	read, ok := inspections[cwd]
	if !ok || !read.Answered || read.IsSettled() {
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
func (m *model) buildCheckoutParams(task panel.Task) checkoutFieldsParams {
	worktree, read := taskWorktree(m.inspections, task.Cwd)

	return checkoutFieldsParams{
		task:     task,
		worktree: worktree,
		read:     read,
		now:      m.clock.Now(),
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

	if read.Item.Readable {
		fields = append(fields, record.Field{Label: "changes", Value: changesField(read)})
		if read.Landed > 0 {
			fields = append(fields, record.Field{Label: "landed", Value: fmt.Sprintf("%s the base already holds", label.Count(read.Landed, "commit", "commits"))})
		}
		if read.Sequencing {
			fields = append(fields, record.Field{Label: "sequence", Value: "a merge or rebase is part-way through"})
		}
	}

	return append(fields, record.Field{Label: "size", Value: sizeField(read.Item, now)})
}

// hoveredRecordAgent is the agent the record under the agents list or the archive is about, which
// for a task is its writer, and none on any other screen or row.
func (m *model) hoveredRecordAgent() (panel.AgentRow, bool) {
	screen, ok := m.active().(hoveredAgentReporter)
	if !ok {
		return panel.AgentRow{}, false
	}

	row, ok := screen.hoveredRow(m, m.inputs())
	if !ok {
		return panel.AgentRow{}, false
	}
	if row.isTask() {
		return m.listing.FindWriterInCheckout(*row.task)
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

	row, ok := screen.hoveredRow(m, m.inputs())
	switch {
	case !ok:
		return panel.Task{}, false
	case row.isTask():
		return *row.task, true
	case row.header || row.underTask:
		return panel.Task{}, false
	}

	return m.listing.Tasks.FindForAgent(row.item.Agent), true
}

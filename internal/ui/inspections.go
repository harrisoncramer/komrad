package ui

import (
	"context"
	"log/slog"
	"time"

	"komrad/internal/logging"
	"komrad/internal/ui/api"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	loopv1 "komrad/proto/gen/loop/v1"

	tea "charm.land/bubbletea/v2"
)

// readWorktreeCmd asks the server for one directory's standing, once at a time.
func (m *model) readWorktreeCmd(dir string) tea.Cmd {
	last, asked := m.inspections[dir]
	if asked && last.Pending {
		return nil
	}

	pending := last
	pending.Pending = true
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: dir, inspection: pending})
	clients, clock := m.api, m.clock

	return func() tea.Msg {
		read, err := clients.GetWorktree(dir)
		if err != nil {
			// The last answer stays on the row with the refusal beside it.
			last.Answered, last.ReadAt, last.Err = true, clock.Now(), err

			return worktreeInspectedMsg{dir: dir, inspection: last}
		}

		return worktreeInspectedMsg{dir: dir, inspection: worktreeInspectionFrom(read, clock.Now())}
	}
}

// worktreeInspectedMsg is one directory's standing come back.
type worktreeInspectedMsg struct {
	dir        string
	inspection panel.WorktreeInspection
}

func (m *model) applyWorktreeInspection(msg worktreeInspectedMsg) {
	if m.inspections == nil {
		m.inspections = map[string]panel.WorktreeInspection{}
	}

	m.inspections[msg.dir] = msg.inspection
}

// forgetUnlistedInspections forgets the readings of directories that have left the listing.
func (m *model) forgetUnlistedInspections() {
	listedDirs := make(map[string]bool, len(m.listing.Worktrees))
	for _, item := range m.listing.Worktrees {
		listedDirs[item.Dir] = true
	}
	for dir := range m.inspections {
		if !listedDirs[dir] {
			delete(m.inspections, dir)
		}
	}
}

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

// hoveredRecordAgent is the agent the record under the agents list or the archive is about, which
// for a task is its writer, and none on any other screen or row.
func (m *model) hoveredRecordAgent() (panel.AgentRow, bool) {
	r, ok := m.active().(hoveredAgentReporter)
	if !ok {
		return panel.AgentRow{}, false
	}

	return r.RecordAgent(m.inputs())
}

// hoveredRecordTask is the task whose checkout the record under the agents list or the archive
// shows, and none on any other screen or row.
func (m *model) hoveredRecordTask() (panel.Task, bool) {
	r, ok := m.active().(hoveredAgentReporter)
	if !ok {
		return panel.Task{}, false
	}

	return r.RecordTask(m.inputs())
}

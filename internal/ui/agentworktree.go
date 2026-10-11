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
	"komrad/lib/xslice"
	loopv1 "komrad/proto/gen/loop/v1"
	taskv1 "komrad/proto/gen/task/v1"

	tea "charm.land/bubbletea/v2"
)

// worktreeReadTTL is how long a reading of an agent's directory stands before the cursor resting
// on the row asks for it again.
const worktreeReadTTL = time.Minute

// inspectAgentWorktreeCmd asks for the hovered agent's directory where nothing stands for it or
// what stands is stale.
func (m *model) inspectAgentWorktreeCmd(now time.Time) tea.Cmd {
	item, ok := m.hoveredRecordAgent()
	if !ok || item.Agent.Cwd == "" {
		return nil
	}
	if read, asked := m.inspections[item.Agent.Cwd]; asked && !read.isStale(now) {
		return nil
	}

	return m.readWorktreeCmd(item.Agent.Cwd)
}

// wakeCwdSyncCmd wakes the cwd sync while the cursor rests on a live agent.
func (m *model) wakeCwdSyncCmd() tea.Cmd {
	item, ok := m.hoveredRecordAgent()
	if !ok || item.Agent.Cwd == "" || item.Agent.ArchivedAt != nil {
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

// rereadAgentWorktreeCmd asks for the hovered agent's directory again however fresh what stands
// for it is, short of an answer that will never change.
func (m *model) rereadAgentWorktreeCmd() tea.Cmd {
	item, ok := m.hoveredRecordAgent()
	if !ok || item.Agent.Cwd == "" || m.inspections[item.Agent.Cwd].isSettled() {
		return nil
	}

	return m.readWorktreeCmd(item.Agent.Cwd)
}

// agentWorktree is what has been read of an agent's directory, and nothing for one that is not
// a worktree komrad holds.
func agentWorktree(inspections map[string]worktreeInspection, cwd string) (worktreeInspection, bool) {
	read, ok := inspections[cwd]
	if !ok || !read.answered || read.isSettled() {
		return worktreeInspection{}, false
	}

	return read, true
}

// worktreeStandingFields are the record's rows for where an agent's directory stands: what is
// uncommitted in it, what it takes on disk and who else works there, with the refusal above them
// when the last read failed.
func worktreeStandingFields(read worktreeInspection, agentID string, now time.Time) []recordField {
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

	fields = append(fields, recordField{label: "size", value: sizeField(read.item, now)})
	others := xslice.Filter(read.item.occupants, func(occupant worktreeAgent) bool { return occupant.id != agentID })

	return appendOccupantsField(fields, "also here", others)
}

// agentDetailsTags is what the record's rule carries for an agent.
func agentDetailsTags(m *model, item panel.AgentRow) []string {
	worktree, _ := agentWorktree(m.inspections, item.Agent.Cwd)

	return append(buildWorktreeStateTags(worktree), buildDiffStatTag(item.DiffStat))
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
	if ok && row.isTask() {
		return m.listing.findTaskWriter(*row.task, *row.lead), true
	}
	if !ok || row.header {
		return panel.AgentRow{}, false
	}

	return row.item, true
}

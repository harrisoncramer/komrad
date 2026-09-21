package ui

import (
	"fmt"
	"time"

	"weave/internal/errui"
	"weave/internal/ui/paint/label"

	tea "github.com/charmbracelet/bubbletea"
)

// worktreeReadTTL is how long a reading of an agent's directory stands before the cursor resting
// on the row asks for it again.
const worktreeReadTTL = time.Minute

// worktreeRowsShown is how many uncommitted paths and how many commits the record lists before
// counting the rest.
const worktreeRowsShown = 10

// inspectAgentWorktreeCmd asks for the hovered agent's directory where nothing stands for it or
// what stands is stale.
func (m *model) inspectAgentWorktreeCmd(now time.Time) tea.Cmd {
	item, ok := m.hoveredRecordAgent()
	if !ok || item.agent.Cwd == "" {
		return nil
	}
	if read, asked := m.inspections[item.agent.Cwd]; asked && !read.isStale(now) {
		return nil
	}

	return m.readWorktreeCmd(item.agent.Cwd)
}

// rereadAgentWorktreeCmd asks for the hovered agent's directory again however fresh what stands
// for it is, short of an answer that will never change.
func (m *model) rereadAgentWorktreeCmd() tea.Cmd {
	item, ok := m.hoveredRecordAgent()
	if !ok || item.agent.Cwd == "" || m.inspections[item.agent.Cwd].isSettled() {
		return nil
	}

	return m.readWorktreeCmd(item.agent.Cwd)
}

// agentWorktree is what has been read of an agent's directory, and nothing for one that is not
// a worktree weave holds.
func (m model) agentWorktree(cwd string) (worktreeInspection, bool) {
	read, ok := m.inspections[cwd]
	if !ok || !read.answered || read.isSettled() {
		return worktreeInspection{}, false
	}

	return read, true
}

// worktreeStandingFields are the record's rows for where the directory stands: against its base,
// what is uncommitted in it, and what it takes on disk, with the refusal above them when the
// last read failed.
func worktreeStandingFields(read worktreeInspection, now time.Time) []recordField {
	fields := []recordField{}
	if read.err != nil {
		fields = append(fields, recordField{"worktree", "could not read: " + errui.GetReason(read.err)})
	}
	if !read.isRead() {
		return fields
	}

	if read.item.readable {
		fields = append(fields,
			recordField{"base", baseField(read)},
			recordField{"changes", changesField(read)},
		)
		if read.landed > 0 {
			fields = append(fields, recordField{"landed", fmt.Sprintf("%s the base already holds", label.Count(read.landed, "commit", "commits"))})
		}
		if read.sequencing {
			fields = append(fields, recordField{"sequence", "a merge or rebase is part-way through"})
		}
	}

	fields = append(fields, recordField{"size", sizeField(read.item, now)})
	if len(read.item.occupants) > 0 {
		fields = append(fields, recordField{"also here", occupantsField(read.item)})
	}

	return fields
}

// worktreeHistoryFields are the record's rows for what the directory holds that the base does
// not: the uncommitted paths, then the commits since the base, each list cut and counted.
func worktreeHistoryFields(read worktreeInspection) []recordField {
	if !read.isRead() || !read.item.readable {
		return nil
	}

	fields := labelledRows("uncommitted", cutRows(changeLines(read), worktreeRowsShown))
	if read.baseRef != "" && len(read.commits) > 0 {
		fields = append(fields, labelledRows("commits", cutRows(commitLines(read.commits), worktreeRowsShown))...)
	}

	return fields
}

// labelledRows is one field per line, the label on the first and the rest under it.
func labelledRows(key string, lines []string) []recordField {
	fields := make([]recordField, 0, len(lines))
	for i, line := range lines {
		if i > 0 {
			key = ""
		}

		fields = append(fields, recordField{key, line})
	}

	return fields
}

// cutRows is the first lines and one counting the rest.
func cutRows(lines []string, shown int) []string {
	if len(lines) <= shown {
		return lines
	}

	return append(lines[:shown:shown], fmt.Sprintf("+ %d more", len(lines)-shown))
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// worktreeReadTTL is how long a reading of an agent's directory stands before the cursor resting
// on the row asks for it again.
const worktreeReadTTL = time.Minute

// worktreeRowsShown is how many commits the record lists before counting the rest.
const worktreeRowsShown = 10

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

// worktreeStandingFields are the record's rows for where the directory stands: against its remote
// branch, what is uncommitted in it, and what it takes on disk, with the refusal above them when
// the last read failed.
func worktreeStandingFields(read worktreeInspection, now time.Time) []recordField {
	fields := []recordField{}
	if read.err != nil {
		fields = append(fields, recordField{label: "worktree", value: "could not read: " + errui.GetReason(read.err)})
	}
	if !read.isRead() {
		return fields
	}

	if read.item.readable {
		if read.item.branch != "" {
			fields = append(fields, buildRemoteField(read))
		}
		fields = append(fields, recordField{label: "changes", value: changesField(read)})
		if read.landed > 0 {
			fields = append(fields, recordField{label: "landed", value: fmt.Sprintf("%s the base already holds", label.Count(read.landed, "commit", "commits"))})
		}
		if read.sequencing {
			fields = append(fields, recordField{label: "sequence", value: "a merge or rebase is part-way through"})
		}
	}

	fields = append(fields, recordField{label: "size", value: sizeField(read.item, now)})
	if len(read.item.occupants) > 0 {
		fields = append(fields, recordField{label: "also here", value: occupantsField(read.item)})
	}

	return fields
}

// worktreeHistoryFields are the record's rows for the commits since the base, cut and counted.
func worktreeHistoryFields(read worktreeInspection) []recordField {
	if !read.isRead() || !read.item.readable || read.baseRef == "" || len(read.commits) == 0 {
		return nil
	}

	return labelledRows("commits", cutRows(commitLines(read.commits), worktreeRowsShown))
}

// buildRemoteField is where the branch stands against its copy on origin.
func buildRemoteField(read worktreeInspection) recordField {
	warning := paint.Panel.Style(paint.ToneWarning)
	switch {
	case !read.pushed && read.baseRef != "" && read.ahead == 0:
		return recordField{
			label:  "remote",
			value:  "nothing to push",
			styled: dimStyle.Render("nothing to push"),
		}
	case !read.pushed:
		return recordField{
			label:  "remote",
			value:  "not pushed",
			styled: warning.Render("not pushed"),
		}
	case read.unpushed == 0 && read.unpulled == 0:
		return recordField{
			label:  "remote",
			value:  "even",
			styled: dimStyle.Render("even"),
		}
	}

	unpushed := fmt.Sprintf("↑%d unpushed", read.unpushed)
	unpulled := fmt.Sprintf("↓%d unpulled", read.unpulled)
	if read.unpulledLanded {
		landed := fmt.Sprintf("%s · landed on %s", unpulled, strings.TrimPrefix(read.baseRef, "origin/"))
		pushing := dimStyle
		if read.ahead > 0 {
			pushing = countStyle(read.unpushed, warning.Bold(true))
		}

		return recordField{
			label:  "remote",
			value:  unpushed + " " + landed,
			styled: pushing.Render(unpushed) + " " + dimStyle.Render(landed),
		}
	}

	return recordField{
		label:  "remote",
		value:  unpushed + " " + unpulled,
		styled: countStyle(read.unpushed, warning.Bold(true)).Render(unpushed) + " " + countStyle(read.unpulled, paint.Panel.Style(paint.ToneMark)).Render(unpulled),
	}
}

// buildDiffStatTag is the lines the branch adds and removes since its base, and nothing where no
// base could be measured against.
func buildDiffStatTag(read worktreeInspection, ok bool) string {
	if !ok || !read.isRead() || !read.item.readable || !read.measured {
		return ""
	}
	if read.added == 0 && read.removed == 0 {
		return dimStyle.Render("clean")
	}

	return paint.Panel.Style(paint.ToneSuccess).Render(fmt.Sprintf("+%d", read.added)) + " " + paint.Panel.Style(paint.ToneDanger).Render(fmt.Sprintf("−%d", read.removed))
}

func countStyle(count int, style lipgloss.Style) lipgloss.Style {
	if count == 0 {
		return dimStyle
	}

	return style
}

// labelledRows is one field per line, the label on the first and the rest under it.
func labelledRows(key string, lines []string) []recordField {
	fields := make([]recordField, 0, len(lines))
	for i, line := range lines {
		if i > 0 {
			key = ""
		}

		fields = append(fields, recordField{label: key, value: line})
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

// hoveredRecordAgent is the agent the record under the agents list or the archive is about, and
// none on any other screen or row.
func (m model) hoveredRecordAgent() (panel.AgentRow, bool) {
	var row agentRow
	var ok bool
	switch m.screen {
	case screenAgents:
		row, ok = m.agents.hoveredRow(m.shared())
	case screenArchive:
		row, ok = rowAt(m.archive.listRows(m.shared()), m.archive.cursor)
	case screenSchedules, screenSearch, screenReview, screenWorktrees:
		return panel.AgentRow{}, false
	}
	if !ok || row.header {
		return panel.AgentRow{}, false
	}

	return row.item, true
}

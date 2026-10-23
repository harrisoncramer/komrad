package ui

import (
	"slices"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// settleWait is how long an answered write waits for the panel to show what it did before it finishes regardless.
const settleWait = warmupCap

// parkedWrite is an answered write waiting for the panel to show what it did.
type parkedWrite struct {
	result   writeResult
	deadline time.Time
}

// awaitsOf is everything a landed write waits on: the rows it named, the live pane it made with
// its harness warm, and the rows of the agents it killed gone.
func awaitsOf(landed screen.WriteResult, unstaged []panel.Agent) screen.WaitFor {
	awaits := landed.WaitFor
	if made := landed.CreatedRow; made != nil && !made.Archived {
		awaits.Pane, awaits.Harness = made.Pane, made.Harness
	}

	if landed.Killed {
		awaits.RowsGone = append(awaits.RowsGone, screen.RowsIn(screen.ListAgents, panel.IDs(unstaged)...)...)
	}

	return awaits
}

// isSettled reports whether the panel shows everything a write waits on.
func (m *model) isSettled(a screen.WaitFor) bool {
	if slices.ContainsFunc(a.RowsShown, func(row screen.Row) bool { return !isListed(m.listing, row) }) {
		return false
	}

	if slices.ContainsFunc(a.RowsGone, func(row screen.Row) bool { return isListed(m.listing, row) }) {
		return false
	}

	return a.Pane == "" || m.isPaneWarm(a.Pane, a.Harness)
}

// isListed reports whether the listing has a row.
func isListed(l panel.Listing, row screen.Row) bool {
	switch row.List {
	case screen.ListAgents:
		return hasAgentRow(l.Agents, row.ID)
	case screen.ListArchivedAgents:
		return hasAgentRow(l.ArchivedAgents, row.ID)
	case screen.ListWorkflows:
		return hasWorkflow(l.Workflows, row.ID)
	case screen.ListArchivedWorkflows:
		return hasWorkflow(l.ArchivedWorkflows, row.ID)
	case screen.ListSchedules:
		return hasSchedule(l.Schedules, row.ID)
	case screen.ListArchivedSchedules:
		return hasSchedule(l.ArchivedSchedules, row.ID)
	case screen.ListWorktrees:
		return slices.ContainsFunc(l.Worktrees, func(item panel.Worktree) bool { return item.Dir == row.ID })
	}

	return false
}

func hasAgentRow(rows []panel.AgentRow, id string) bool {
	return slices.ContainsFunc(rows, func(row panel.AgentRow) bool { return row.Agent.ID == id })
}

func hasWorkflow(workflows []panel.Workflow, id string) bool {
	return slices.ContainsFunc(workflows, func(workflow panel.Workflow) bool { return workflow.ID == id })
}

func hasSchedule(schedules []panel.Schedule, id string) bool {
	return slices.ContainsFunc(schedules, func(schedule panel.Schedule) bool { return schedule.ID == id })
}

// parkWrite keeps an answered write in flight, and its form up, until the panel shows what it did.
func (m *model) parkWrite(result writeResult) {
	if m.writes.parked == nil {
		m.writes.parked = map[int]parkedWrite{}
	}

	m.writes.parked[result.write] = parkedWrite{
		result:   result,
		deadline: m.clock.Now().Add(settleWait),
	}
}

// finishParked finishes, in the order they were sent and in one change, the parked writes that have
// settled or run out of time.
func (m *model) finishParked() tea.Cmd {
	if len(m.writes.parked) == 0 {
		return nil
	}

	now := m.clock.Now()
	var ready []int
	for write, parked := range m.writes.parked {
		if m.isSettled(parked.result.awaits) || now.After(parked.deadline) {
			ready = append(ready, write)
		}
	}
	slices.Sort(ready)

	m.frame.Begin()
	defer m.frame.Flush()

	var cmds []tea.Cmd
	for _, write := range ready {
		parked := m.writes.parked[write]
		delete(m.writes.parked, write)
		cmds = append(cmds, m.finishWrite(parked.result))
	}

	return tea.Batch(cmds...)
}

// isPaneAwaited reports whether a parked write is waiting on a pane's harness to render itself.
func (m *model) isPaneAwaited(pane string) bool {
	for _, parked := range m.writes.parked {
		if parked.result.awaits.Pane == pane {
			return true
		}
	}

	return false
}

// releaseCursor lets the cursor stay where a key moved it when a parked write no form owns finishes.
func (m *model) releaseCursor() {
	for write, parked := range m.writes.parked {
		if m.writes.held.owns(write) {
			continue
		}

		parked.result.keyMoved = true
		m.writes.parked[write] = parked
	}
}

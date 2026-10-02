package ui

import (
	"slices"
	"strings"
	"time"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// settleWait is how long an answered write waits for the panel to show what it did before it finishes regardless.
const settleWait = warmupCap

// rowList is which of the listing's lists a row is looked for in.
type rowList int

const (
	listAgents rowList = iota
	listArchivedAgents
	listWorkflows
	listArchivedWorkflows
	listSchedules
	listArchivedSchedules
	listWorktrees
)

// listedRow is one row of the listing, by the list it sits in and its id.
type listedRow struct {
	list rowList
	id   string
}

// awaits is what the panel shows once a write is answered, and the write is held until it does.
type awaits struct {
	shown []listedRow
	gone  []listedRow
	// pane is the pane whose harness is to have drawn itself, empty when there is none to wait on.
	pane    string
	harness string
}

// parkedWrite is an answered write waiting for the panel to show what it did.
type parkedWrite struct {
	result   writeResult
	deadline time.Time
}

// rowsIn names the given ids as rows of one list.
func rowsIn(list rowList, ids ...string) []listedRow {
	return xslice.Map(ids, func(id string) listedRow { return listedRow{list: list, id: id} })
}

// awaitShown waits for the listing to hold the row a write made, with its pane warm when it has one to show.
func (w written) awaitShown() *awaits {
	if w.archived {
		return &awaits{shown: rowsIn(listArchivedAgents, w.agent)}
	}

	shown := rowsIn(listAgents, w.agent)
	if id, ok := strings.CutPrefix(w.row, workflowRowPrefix); ok {
		shown = rowsIn(listWorkflows, id)
	}

	return &awaits{
		shown:   shown,
		pane:    w.pane,
		harness: w.harness,
	}
}

// isSettled reports whether the panel shows everything a write waits on.
func (a *awaits) isSettled(m *model) bool {
	if slices.ContainsFunc(a.shown, func(row listedRow) bool { return !m.listing.holds(row) }) {
		return false
	}

	if slices.ContainsFunc(a.gone, m.listing.holds) {
		return false
	}

	return a.pane == "" || m.isPaneWarm(a.pane, a.harness)
}

// holds reports whether the listing has a row.
func (l listing) holds(row listedRow) bool {
	switch row.list {
	case listAgents:
		return hasAgentRow(l.agents, row.id)
	case listArchivedAgents:
		return hasAgentRow(l.archivedAgents, row.id)
	case listWorkflows:
		return hasWorkflow(l.workflows, row.id)
	case listArchivedWorkflows:
		return hasWorkflow(l.archivedWorkflows, row.id)
	case listSchedules:
		return hasSchedule(l.schedules, row.id)
	case listArchivedSchedules:
		return hasSchedule(l.archivedSchedules, row.id)
	case listWorktrees:
		return slices.ContainsFunc(l.worktrees, func(item worktreeItem) bool { return item.dir == row.id })
	}

	return false
}

func hasAgentRow(rows []panel.AgentRow, id string) bool {
	return slices.ContainsFunc(rows, func(row panel.AgentRow) bool { return row.Agent.ID == id })
}

func hasWorkflow(workflows []panel.Workflow, id string) bool {
	return slices.ContainsFunc(workflows, func(workflow panel.Workflow) bool { return workflow.ID == id })
}

func hasSchedule(items []scheduleItem, id string) bool {
	return slices.ContainsFunc(items, func(item scheduleItem) bool { return item.key() == id })
}

// parkWrite keeps an answered write in flight, and its form up, until the panel shows what it did.
func (m *model) parkWrite(result writeResult) {
	if m.writes.parked == nil {
		m.writes.parked = map[int]parkedWrite{}
	}

	m.writes.parked[result.write] = parkedWrite{
		result:   result,
		deadline: time.Now().Add(settleWait),
	}
}

// finishParked finishes, in the order they were sent and in one change, the parked writes that have
// settled or run out of time.
func (m *model) finishParked() tea.Cmd {
	if len(m.writes.parked) == 0 {
		return nil
	}

	now := time.Now()
	var ready []int
	for write, held := range m.writes.parked {
		if held.result.awaits.isSettled(m) || now.After(held.deadline) {
			ready = append(ready, write)
		}
	}
	slices.Sort(ready)

	m.frame.Begin()
	defer m.frame.Flush()

	var cmds []tea.Cmd
	for _, write := range ready {
		held := m.writes.parked[write]
		delete(m.writes.parked, write)
		cmds = append(cmds, m.finishWrite(held.result))
	}

	return tea.Batch(cmds...)
}

// isPaneAwaited reports whether a parked write is waiting on a pane's harness to draw itself.
func (m *model) isPaneAwaited(pane string) bool {
	for _, held := range m.writes.parked {
		if held.result.awaits.pane == pane {
			return true
		}
	}

	return false
}

// releaseCursor lets the cursor stay where a key moved it when a parked write no form owns finishes.
func (m *model) releaseCursor() {
	for write, held := range m.writes.parked {
		if m.writes.held.owns(write) {
			continue
		}

		held.result.keyMoved = true
		m.writes.parked[write] = held
	}
}

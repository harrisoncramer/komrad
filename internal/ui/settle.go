package ui

import (
	"slices"
	"strings"
	"time"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// settleWait is how long a landed write waits for the panel to show what it did before it lands regardless.
const settleWait = warmupCap

// settle says when the panel shows what a landed write did.
type settle interface {
	isSettled(m *model) bool
}

// rowShown settles once the listing holds every row a write made.
type rowShown struct {
	agents   []string
	workflow string
	// archived says the agents are looked for in the archive rather than the agents list.
	archived bool
}

func (r rowShown) isSettled(m *model) bool {
	if r.workflow != "" && !hasWorkflow(m.listing.workflows, r.workflow) {
		return false
	}

	rows := m.listing.agents
	if r.archived {
		rows = m.listing.archivedAgents
	}

	return !slices.ContainsFunc(r.agents, func(id string) bool { return !hasAgentRow(rows, id) })
}

// paneWarm settles once a write's row is shown and the harness in its pane has drawn itself.
type paneWarm struct {
	pane    string
	harness string
	row     rowShown
}

func (p paneWarm) isSettled(m *model) bool {
	return p.row.isSettled(m) && m.isPaneWarm(p.pane, p.harness)
}

// rowsGone settles once the listing holds none of the rows a write took away.
type rowsGone struct {
	agents            []string
	archivedAgents    []string
	workflows         []string
	archivedWorkflows []string
	dirs              []string
}

func (r rowsGone) isSettled(m *model) bool {
	for _, id := range r.agents {
		if hasAgentRow(m.listing.agents, id) {
			return false
		}
	}

	for _, id := range r.archivedAgents {
		if hasAgentRow(m.listing.archivedAgents, id) {
			return false
		}
	}

	for _, id := range r.workflows {
		if hasWorkflow(m.listing.workflows, id) {
			return false
		}
	}

	for _, id := range r.archivedWorkflows {
		if hasWorkflow(m.listing.archivedWorkflows, id) {
			return false
		}
	}

	return !slices.ContainsFunc(m.listing.worktrees, func(item worktreeItem) bool { return slices.Contains(r.dirs, item.dir) })
}

func hasAgentRow(rows []panel.AgentRow, id string) bool {
	return slices.ContainsFunc(rows, func(row panel.AgentRow) bool { return row.Agent.ID == id })
}

func hasWorkflow(workflows []panel.Workflow, id string) bool {
	return slices.ContainsFunc(workflows, func(workflow panel.Workflow) bool { return workflow.ID == id })
}

// shownOf is what settles once the listing holds the row a write made.
func shownOf(row written) rowShown {
	if id, ok := strings.CutPrefix(row.row, workflowRowPrefix); ok {
		return rowShown{workflow: id}
	}

	return rowShown{agents: []string{row.agent}, archived: row.archived}
}

// awaitsOf is what settles once the panel shows the row a write made, with its pane warm when it has one to show.
func awaitsOf(row written) settle {
	shown := shownOf(row)
	if row.pane == "" || row.archived {
		return shown
	}

	return paneWarm{pane: row.pane, harness: row.harness, row: shown}
}

// awaitingCmd has a write's result wait on awaits before it lands.
func awaitingCmd(cmd tea.Cmd, awaits settle) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.awaits = awaits

		return result
	}
}

// parked is a landed write waiting for the panel to show what it did.
type parked struct {
	result   writeResult
	deadline time.Time
	// claimsCursor says the cursor still goes to the row the write made once it lands.
	claimsCursor bool
}

type settleTickMsg struct{}

// parkWrite keeps a landed write in flight, and its form up, until the panel shows what it did.
func (m *model) parkWrite(result writeResult) tea.Cmd {
	if m.writes.settling == nil {
		m.writes.settling = map[int]parked{}
	}

	m.writes.settling[result.write] = parked{result: result, deadline: time.Now().Add(settleWait), claimsCursor: true}

	return m.pollSettling()
}

// pollSettling keeps a tick going while a write is parked.
func (m *model) pollSettling() tea.Cmd {
	if m.writes.settlePolling || len(m.writes.settling) == 0 {
		return nil
	}

	m.writes.settlePolling = true

	return startTimer(warmupPoll, func(time.Time) tea.Msg { return settleTickMsg{} })
}

// applySettleTick lands the parked writes that have settled and keeps polling for the rest.
func (m *model) applySettleTick() tea.Cmd {
	m.writes.settlePolling = false

	return tea.Batch(m.settleParked(), m.pollSettling())
}

// settleParked lands, in the order they were sent, the parked writes that have settled or run out of time.
func (m *model) settleParked() tea.Cmd {
	if len(m.writes.settling) == 0 {
		return nil
	}

	now := time.Now()
	var ready []int
	for write, held := range m.writes.settling {
		if held.result.awaits.isSettled(m) || now.After(held.deadline) {
			ready = append(ready, write)
		}
	}
	slices.Sort(ready)

	var cmds []tea.Cmd
	for _, write := range ready {
		held := m.writes.settling[write]
		delete(m.writes.settling, write)
		cmds = append(cmds, m.finishWrite(held.result, held.claimsCursor))
	}

	return tea.Batch(cmds...)
}

// releaseCursor lets the cursor stay where a key moved it when a parked write no form owns lands.
func (m *model) releaseCursor() {
	for write, held := range m.writes.settling {
		if m.writes.held.owns(write) {
			continue
		}

		held.claimsCursor = false
		m.writes.settling[write] = held
	}
}

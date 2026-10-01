package ui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

const (
	triageHeaderKey  = "triage:header"
	triageMoreKey    = "triage:more"
	triageDividerKey = "triage:divider"
)

// triageRule is one reason an agent is waiting on a person.
type triageRule struct {
	reason string
	tone   paint.Tone
	is     func(panel.AgentRow) bool
}

// triageRules are the reasons an agent needs triage, the first that holds naming it.
var triageRules = []triageRule{
	{
		reason: "asking",
		tone:   paint.ToneWarning,
		is:     func(item panel.AgentRow) bool { return displayState(item) == panel.StateWaiting },
	},
	{
		reason: "errored",
		tone:   paint.ToneDanger,
		is:     func(item panel.AgentRow) bool { return displayState(item) == panel.StateErrored },
	},
	{
		reason: "launch failed",
		tone:   paint.ToneDanger,
		is:     func(item panel.AgentRow) bool { return item.Agent.State == panel.StateFailedToCreate },
	},
	{
		reason: "cleanup failed",
		tone:   paint.ToneDanger,
		is:     func(item panel.AgentRow) bool { return item.Agent.State == panel.StateFailedToCleanup },
	},
	{
		reason: "script failed",
		tone:   paint.ToneDanger,
		is:     func(item panel.AgentRow) bool { return item.FailedRun },
	},
	{
		reason: "conflict",
		tone:   paint.ToneWarning,
		is: func(item panel.AgentRow) bool {
			return item.Agent.SequenceInProgress && displayState(item) != panel.StateThinking
		},
	},
}

// triageZone is the agents waiting on a person, drawn ahead of the tree while it has room for any.
type triageZone struct {
	// capacity is how many agents the zone draws, and zero turns it off.
	capacity int
	// entries are the agents in triage by id.
	entries map[string]triageEntry
	// focused says the keyboard is inside the zone.
	focused bool
	// returnTo is the row the cursor stood on when the zone took the keyboard.
	returnTo string
}

// triageEntry is why an agent needs triage and since when.
type triageEntry struct {
	since time.Time
	rule  triageRule
}

// isOn reports whether the zone is drawn at all.
func (z *triageZone) isOn() bool {
	return z.capacity > 0
}

// refresh reads which agents need triage from the listing as it stands now, keeping the agent behind
// the row named where it was.
func (z *triageZone) refresh(agents []panel.AgentRow, keep string) {
	if !z.isOn() {
		z.entries = nil

		return
	}

	entries := make(map[string]triageEntry)
	for _, item := range agents {
		id := item.Agent.ID
		if item.Agent.Hidden || item.Agent.HeldAt != nil {
			continue
		}

		was, wasQueued := z.entries[id]
		if wasQueued && agentRowKey(id) == keep {
			entries[id] = was

			continue
		}

		// An agent komrad cannot read stays where it was.
		if displayState(item) == panel.StateUncertain {
			if wasQueued {
				entries[id] = was
			}

			continue
		}

		rule, ok := findTriageRule(item)
		if !ok {
			continue
		}

		since := item.Agent.StateChangedAt
		if wasQueued {
			since = was.since
		}

		entries[id] = triageEntry{since: since, rule: rule}
	}

	z.entries = entries
}

// findTriageRule is the first rule an agent meets.
func findTriageRule(item panel.AgentRow) (triageRule, bool) {
	for _, rule := range triageRules {
		if rule.is(item) {
			return rule, true
		}
	}

	return triageRule{}, false
}

// agentsLayout is the agents list split into triage and the tree under it.
type agentsLayout struct {
	zone []agentRow
	// waiting is how many agents in triage the zone has no room for.
	waiting int
	tree    []agentRow
}

// cursorRows are the rows the keyboard can reach, only the zone's while it holds the keyboard.
func (l agentsLayout) cursorRows(z triageZone) []agentRow {
	if z.focused {
		return l.zone
	}

	return slices.Concat(l.zone, l.tree)
}

// splitTriage takes the oldest agents in triage the zone has room for out of the listing.
func splitTriage(z triageZone, s panelShared) (zone []agentRow, waiting int, rest []panel.AgentRow) {
	queued := xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool {
		_, ok := z.entries[item.Agent.ID]

		return ok
	})
	slices.SortFunc(queued, func(a, b panel.AgentRow) int {
		return cmp.Or(z.entries[a.Agent.ID].since.Compare(z.entries[b.Agent.ID].since), cmp.Compare(a.Agent.ID, b.Agent.ID))
	})

	slotted := queued[:min(z.capacity, len(queued))]
	byID := xslice.KeyBy(s.listing.workflows, func(w panel.Workflow) string { return w.ID })
	zone = xslice.Map(slotted, func(item panel.AgentRow) agentRow {
		rule := z.entries[item.Agent.ID].rule

		return agentRow{item: item, workflow: byID[item.Agent.WorkflowID], triage: &rule}
	})

	inZone := make(map[string]bool, len(slotted))
	for _, item := range slotted {
		inZone[item.Agent.ID] = true
	}
	rest = xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool { return !inZone[item.Agent.ID] })

	return zone, len(queued) - len(slotted), rest
}

// toggleTriage moves the keyboard into the zone, or back out to where it was.
func (a *agentsScreen) toggleTriage(h host, s panelShared) {
	if a.triage.focused {
		a.leaveTriage(s)
		h.stageSelection()

		return
	}

	layout := a.layout(s)
	if len(layout.zone) == 0 {
		h.note(messageInfo, "nothing to triage")

		return
	}

	a.triage.returnTo = rowKeyAt(layout.cursorRows(a.triage), a.cursor)
	a.triage.focused = true
	a.cursor = 0
	h.stageSelection()
}

// leaveTriage gives the keyboard back to the tree, on the row it was taken from.
func (a *agentsScreen) leaveTriage(s panelShared) {
	a.triage.focused = false

	rows := a.rows(s)
	idx := rowIndexOfKey(rows, a.triage.returnTo)
	if idx < 0 {
		idx = firstAgentRowIndex(rows)
	}

	a.cursor = idx
}

// settleTriage gives the keyboard back to the tree once the zone holding it has emptied.
func (a *agentsScreen) settleTriage(h host, s panelShared) {
	if !a.triage.focused || len(a.layout(s).zone) > 0 {
		return
	}

	a.leaveTriage(s)
	h.note(messageInfo, "triage clear")
}

// triageSnapshot is the zone as the agents list draws it.
type triageSnapshot struct {
	zone     []agentRow
	waiting  int
	capacity int
	focused  bool
}

// triageRows are the lines the zone draws ahead of the tree, none while it is empty.
func triageRows(params snapshotParams) []view.Row {
	queue := params.triage
	if len(queue.zone) == 0 {
		return nil
	}

	note := []view.Cell{{Text: fmt.Sprintf("%d/%d", len(queue.zone), queue.capacity), Tone: paint.ToneMuted}}
	if queue.focused {
		note = append(note, view.Cell{Text: "  focused", Tone: paint.ToneWarning})
	}

	rows := []view.Row{{
		Key:    triageHeaderKey,
		Kind:   view.KindTriageHeader,
		Name:   "triage",
		Status: view.StatusWaiting,
		Note:   view.Cell{Parts: note},
	}}

	for _, r := range queue.zone {
		row := params.renaming.drawnOver(snapshotRow(r, params))
		row.Kind = view.KindTriage
		reason := view.Cell{Text: r.triage.reason, Tone: r.triage.tone}
		row.Note = view.Cell{Parts: []view.Cell{
			{Text: r.workflow.Name, Tone: paint.ToneMuted},
			{Text: separated, Tone: paint.ToneMuted},
			reason,
		}}
		if r.item.Agent.Role == panel.RoleOrchestrator {
			row.Name = r.workflow.Name
			row.Note = reason
		}
		rows = append(rows, row)
	}

	if queue.waiting > 0 {
		rows = append(rows, view.Row{
			Key:     triageMoreKey,
			Kind:    view.KindTriageMore,
			Name:    fmt.Sprintf("+%d more waiting for a slot", queue.waiting),
			Process: view.ProcessNone,
			Faded:   true,
		})
	}

	return append(rows, view.Row{Key: triageDividerKey, Kind: view.KindDivider})
}

// releaseTriageRow lets go of the row the triage zone kept while the keyboard was in the pane, moving
// the cursor on when its agent no longer needs a person.
func (m *model) releaseTriageRow() {
	if !m.agents.triage.focused {
		return
	}

	s := m.shared()
	hovered := m.agents.list().hovered(s)
	m.agents.triage.refresh(m.listing.agents, "")
	m.agents.apply(s, hovered)
	m.agents.settleTriage(m, m.shared())

	if m.agents.list().hovered(m.shared()) != hovered && m.screen == screenAgents {
		m.stageSelection()
	}
}

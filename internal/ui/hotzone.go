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
	hotHeaderKey  = "hot:header"
	hotMoreKey    = "hot:more"
	hotDividerKey = "hot:divider"
)

// hotRule is one reason an agent is waiting on a person.
type hotRule struct {
	reason string
	tone   paint.Tone
	is     func(panel.AgentRow) bool
}

// hotRules are the reasons an agent is hot, the first that holds naming it.
var hotRules = []hotRule{
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

// hotZone is the agents waiting on a person, drawn ahead of the tree while it has room for any.
type hotZone struct {
	// capacity is how many agents the zone draws, and zero turns it off.
	capacity int
	// entries are the hot agents by id.
	entries map[string]hotEntry
	// focused says the keyboard is inside the zone.
	focused bool
	// returnTo is the row the cursor stood on when the zone took the keyboard.
	returnTo string
}

// hotEntry is why an agent is hot and since when.
type hotEntry struct {
	since time.Time
	rule  hotRule
}

// isOn reports whether the zone is drawn at all.
func (z *hotZone) isOn() bool {
	return z.capacity > 0
}

// heat reads which agents are hot from the listing as it stands now, keeping the agent behind
// the row named where it was.
func (z *hotZone) heat(agents []panel.AgentRow, keep string) {
	if !z.isOn() {
		z.entries = nil

		return
	}

	entries := make(map[string]hotEntry)
	for _, item := range agents {
		id := item.Agent.ID
		if item.Agent.Hidden || item.Agent.HeldAt != nil {
			continue
		}

		was, wasHot := z.entries[id]
		if wasHot && agentRowKey(id) == keep {
			entries[id] = was

			continue
		}

		// An agent komrad cannot read stays where it was.
		if displayState(item) == panel.StateUncertain {
			if wasHot {
				entries[id] = was
			}

			continue
		}

		rule, ok := findHotRule(item)
		if !ok {
			continue
		}

		since := item.Agent.StateChangedAt
		if wasHot {
			since = was.since
		}

		entries[id] = hotEntry{since: since, rule: rule}
	}

	z.entries = entries
}

// findHotRule is the first rule an agent meets.
func findHotRule(item panel.AgentRow) (hotRule, bool) {
	for _, rule := range hotRules {
		if rule.is(item) {
			return rule, true
		}
	}

	return hotRule{}, false
}

// agentsLayout is the agents list split into the hot zone and the tree under it.
type agentsLayout struct {
	zone []agentRow
	// waiting is how many hot agents the zone has no room for.
	waiting int
	tree    []agentRow
}

// cursorRows are the rows the keyboard can reach, only the zone's while it holds the keyboard.
func (l agentsLayout) cursorRows(z hotZone) []agentRow {
	if z.focused {
		return l.zone
	}

	return slices.Concat(l.zone, l.tree)
}

// splitHot takes the oldest hot agents the zone has room for out of the listing.
func splitHot(z hotZone, s panelShared) (zone []agentRow, waiting int, rest []panel.AgentRow) {
	hot := xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool {
		_, ok := z.entries[item.Agent.ID]

		return ok
	})
	slices.SortFunc(hot, func(a, b panel.AgentRow) int {
		return cmp.Or(z.entries[a.Agent.ID].since.Compare(z.entries[b.Agent.ID].since), cmp.Compare(a.Agent.ID, b.Agent.ID))
	})

	slotted := hot[:min(z.capacity, len(hot))]
	byID := xslice.KeyBy(s.listing.workflows, func(w panel.Workflow) string { return w.ID })
	zone = xslice.Map(slotted, func(item panel.AgentRow) agentRow {
		rule := z.entries[item.Agent.ID].rule

		return agentRow{item: item, workflow: byID[item.Agent.WorkflowID], hot: &rule}
	})

	inZone := make(map[string]bool, len(slotted))
	for _, item := range slotted {
		inZone[item.Agent.ID] = true
	}
	rest = xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool { return !inZone[item.Agent.ID] })

	return zone, len(hot) - len(slotted), rest
}

// toggleHotZone moves the keyboard into the zone, or back out to where it was.
func (a *agentsScreen) toggleHotZone(h host, s panelShared) {
	if a.hot.focused {
		a.leaveHotZone(s)
		h.stageSelection()

		return
	}

	layout := a.layout(s)
	if len(layout.zone) == 0 {
		h.note(messageInfo, "nothing is hot")

		return
	}

	a.hot.returnTo = rowKeyAt(layout.cursorRows(a.hot), a.cursor)
	a.hot.focused = true
	a.cursor = 0
	h.stageSelection()
}

// leaveHotZone gives the keyboard back to the tree, on the row it was taken from.
func (a *agentsScreen) leaveHotZone(s panelShared) {
	a.hot.focused = false

	rows := a.rows(s)
	idx := rowIndexOfKey(rows, a.hot.returnTo)
	if idx < 0 {
		idx = firstAgentRowIndex(rows)
	}

	a.cursor = idx
}

// settleHotZone gives the keyboard back to the tree once the zone holding it has emptied.
func (a *agentsScreen) settleHotZone(h host, s panelShared) {
	if !a.hot.focused || len(a.layout(s).zone) > 0 {
		return
	}

	a.leaveHotZone(s)
	h.note(messageInfo, "hot zone clear")
}

// hotSnapshot is the zone as the agents list draws it.
type hotSnapshot struct {
	zone     []agentRow
	waiting  int
	capacity int
	focused  bool
}

// hotRows are the lines the zone draws ahead of the tree, none while it is empty.
func hotRows(params snapshotParams) []view.Row {
	hot := params.hot
	if len(hot.zone) == 0 {
		return nil
	}

	note := []view.Cell{{Text: fmt.Sprintf("%d/%d", len(hot.zone), hot.capacity), Tone: paint.ToneMuted}}
	if hot.focused {
		note = append(note, view.Cell{Text: "  focused", Tone: paint.ToneWarning})
	}

	rows := []view.Row{{
		Key:    hotHeaderKey,
		Kind:   view.KindHotHeader,
		Name:   "hot",
		Status: view.StatusWaiting,
		Note:   view.Cell{Parts: note},
	}}

	for _, r := range hot.zone {
		row := params.renaming.drawnOver(snapshotRow(r, params))
		row.Kind = view.KindHot
		reason := view.Cell{Text: r.hot.reason, Tone: r.hot.tone}
		row.Note = view.Cell{Parts: []view.Cell{
			{Text: r.workflow.Name, Tone: paint.ToneMuted},
			{Text: " · ", Tone: paint.ToneMuted},
			reason,
		}}
		if r.item.Agent.Role == panel.RoleOrchestrator {
			row.Name = r.workflow.Name
			row.Note = reason
		}
		rows = append(rows, row)
	}

	if hot.waiting > 0 {
		rows = append(rows, view.Row{
			Key:     hotMoreKey,
			Kind:    view.KindHotMore,
			Name:    fmt.Sprintf("+%d more waiting for a slot", hot.waiting),
			Process: view.ProcessNone,
			Faded:   true,
		})
	}

	return append(rows, view.Row{Key: hotDividerKey, Kind: view.KindDivider})
}

// releaseHotRow lets go of the row the hot zone kept while the keyboard was in the pane, moving
// the cursor on when its agent no longer needs a person.
func (m *model) releaseHotRow() {
	if !m.agents.hot.focused {
		return
	}

	s := m.shared()
	hovered := m.agents.list().hovered(s)
	m.agents.hot.heat(m.listing.agents, "")
	m.agents.apply(s, hovered)
	m.agents.settleHotZone(m, m.shared())

	if m.agents.list().hovered(m.shared()) != hovered && m.screen == screenAgents {
		m.stageSelection()
	}
}

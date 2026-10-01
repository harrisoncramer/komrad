package ui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

const (
	triageMoreKey    = "triage:more"
	triageDividerKey = "triage:divider"
)

// triageTier is how urgently an agent wants a person, the most urgent first.
type triageTier int

const (
	tierNeedsYou triageTier = iota + 1
	tierBroken
	tierFinished
)

// triageKind says who can clear what a rule holds on.
type triageKind int

const (
	// kindPerson holds whatever the agent is doing.
	kindPerson triageKind = iota
	// kindAgent holds only once the agent has been told and is not working on it.
	kindAgent
)

// triageClock says where the wait behind a rule starts.
type triageClock int

const (
	// clockState starts when the agent's state last changed.
	clockState triageClock = iota
	// clockSeen starts when the panel first saw the rule hold.
	clockSeen
)

// triageRule is one reason an agent is waiting on a person.
type triageRule struct {
	name  string
	tier  triageTier
	kind  triageKind
	clock triageClock
	is    func(triageSubject) bool
}

// triageSubject is an agent as the rules read it.
type triageSubject struct {
	item             panel.AgentRow
	hasThinkingChild bool
}

// triageRules are the reasons an agent needs triage in order of urgency, the first that holds
// naming it.
var triageRules = []triageRule{
	{
		name:  "asking",
		tier:  tierNeedsYou,
		kind:  kindPerson,
		clock: clockState,
		is:    func(s triageSubject) bool { return displayState(s.item) == panel.StateWaiting },
	},
	{
		name:  "errored",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockState,
		is:    func(s triageSubject) bool { return displayState(s.item) == panel.StateErrored },
	},
	{
		name:  "launch failed",
		tier:  tierBroken,
		kind:  kindPerson,
		clock: clockState,
		is:    func(s triageSubject) bool { return s.item.Agent.State == panel.StateFailedToCreate },
	},
	{
		name:  "cleanup failed",
		tier:  tierBroken,
		kind:  kindPerson,
		clock: clockState,
		is:    func(s triageSubject) bool { return s.item.Agent.State == panel.StateFailedToCleanup },
	},
	{
		name:  "script failed",
		tier:  tierBroken,
		kind:  kindPerson,
		clock: clockSeen,
		is:    func(s triageSubject) bool { return s.item.FailedRun },
	},
	{
		name:  "checkout conflict",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockSeen,
		is: func(s triageSubject) bool {
			return s.item.Agent.Sequence.IsInProgress() && s.item.Agent.ParentAgentID == nil
		},
	},
	{
		name:  "finished",
		tier:  tierFinished,
		kind:  kindAgent,
		clock: clockState,
		is:    isFinishedImplementer,
	},
}

// triageZone is the agents waiting on a person, drawn ahead of the tree while it has room for any.
type triageZone struct {
	// capacity is how many families the zone draws, each head with its children under it, and zero turns it off.
	capacity int
	// entries are the agents in triage by id.
	entries map[string]triageEntry
	// snoozed are the families sunk to the bottom of the zone, by head id, until what they wait on
	// changes.
	snoozed map[string]triageSnooze
	// frozen is the order of the heads drawn while a person reads the zone, which holds still until
	// they leave it, and nil when it is free to move.
	frozen []string
	// held is the head of the family the cursor is on, which keeps its place in the zone after it stops
	// needing triage until the cursor moves to another family.
	held string
}

// triageEntry is why an agent needs triage and since when.
type triageEntry struct {
	// since is when the agent entered its tier, which orders the zone.
	since time.Time
	// began is when this rule last started to hold, which a snooze is tied to.
	began time.Time
	rule  triageRule
}

// triageSnooze is the reason a family was snoozed on.
type triageSnooze struct {
	rule  string
	began time.Time
}

// triageFamily is a head and its children in triage, named by the most urgent entry among them.
type triageFamily struct {
	head  panel.AgentRow
	entry triageEntry
}

// isOn reports whether the zone is drawn at all.
func (z *triageZone) isOn() bool {
	return z.capacity > 0
}

// refresh reads which agents need triage from the listing, timing new waits from now.
func (z *triageZone) refresh(agents []panel.AgentRow, now time.Time) {
	if !z.isOn() {
		z.entries = nil
		z.snoozed = nil

		return
	}

	thinkingParents := findThinkingParents(agents)
	entries := make(map[string]triageEntry)
	for _, item := range agents {
		id := item.Agent.ID
		if item.Agent.Hidden || item.Agent.HeldAt != nil {
			continue
		}

		was, wasQueued := z.entries[id]
		// An agent komrad cannot read stays where it was.
		if displayState(item) == panel.StateUncertain {
			if wasQueued {
				entries[id] = was
			}

			continue
		}

		rule, ok := findTriageRule(triageSubject{item: item, hasThinkingChild: thinkingParents[id]})
		if !ok {
			if wasQueued && z.isHeld(item) {
				entries[id] = was
			}

			continue
		}

		began := rule.startedAt(item, now)
		since := began
		if wasQueued && was.rule.name == rule.name && rule.clock == clockSeen {
			began = was.began
		}
		if wasQueued && was.rule.tier == rule.tier {
			since = was.since
		}

		entries[id] = triageEntry{since: since, began: began, rule: rule}
	}

	z.entries = entries
	z.pruneSnoozes(agents)
}

// isHeld reports whether the agent is in the family the cursor is on.
func (z *triageZone) isHeld(item panel.AgentRow) bool {
	headID := item.Agent.ID
	if parentID := item.Agent.ParentAgentID; parentID != nil {
		headID = *parentID
	}

	return z.held != "" && headID == z.held
}

// pruneSnoozes ends every snooze whose family has left triage or now waits on something else.
func (z *triageZone) pruneSnoozes(agents []panel.AgentRow) {
	byID := xslice.KeyBy(agents, func(item panel.AgentRow) string { return item.Agent.ID })
	families := xslice.KeyBy(findTriageFamilies(z.entries, agents), func(f triageFamily) string { return f.head.Agent.ID })
	for headID, snooze := range z.snoozed {
		if byID[headID].Agent.Hidden {
			continue
		}

		family, ok := families[headID]
		if !ok || !snooze.isFor(family.entry) {
			delete(z.snoozed, headID)
		}
	}
}

// isFor reports whether the snooze still stands for what the family waits on.
func (s triageSnooze) isFor(entry triageEntry) bool {
	return s.rule == entry.rule.name && s.began.Equal(entry.began)
}

// isSnoozed reports whether the family under the head is snoozed.
func (z *triageZone) isSnoozed(headID string) bool {
	_, ok := z.snoozed[headID]

	return ok
}

// toggleSnooze snoozes the agent's family in triage, or wakes it when it is snoozed, reporting
// false when the family is not in triage.
func (z *triageZone) toggleSnooze(item panel.AgentRow, agents []panel.AgentRow) bool {
	family, ok := z.findFamily(item, agents)
	if !ok {
		return false
	}

	if z.isSnoozed(family.head.Agent.ID) {
		delete(z.snoozed, family.head.Agent.ID)

		return true
	}

	z.snooze(family)

	return true
}

// seeFinished snoozes the agent's family when it waits only to be looked at.
func (z *triageZone) seeFinished(item panel.AgentRow, agents []panel.AgentRow) {
	family, ok := z.findFamily(item, agents)
	if ok && family.entry.rule.tier == tierFinished {
		z.snooze(family)
	}
}

// snooze sinks the family to the bottom of the zone until what it waits on changes.
func (z *triageZone) snooze(family triageFamily) {
	if z.snoozed == nil {
		z.snoozed = make(map[string]triageSnooze)
	}

	z.snoozed[family.head.Agent.ID] = triageSnooze{rule: family.entry.rule.name, began: family.entry.began}
}

// findFamily is the family in triage the agent belongs to.
func (z *triageZone) findFamily(item panel.AgentRow, agents []panel.AgentRow) (triageFamily, bool) {
	head := findTriageHead(item, xslice.KeyBy(agents, func(it panel.AgentRow) string { return it.Agent.ID }))
	families := xslice.KeyBy(findTriageFamilies(z.entries, agents), func(f triageFamily) string { return f.head.Agent.ID })
	family, ok := families[head.Agent.ID]

	return family, ok
}

// findTriageFamilies groups the agents in triage under their heads, in the listing's order.
func findTriageFamilies(entries map[string]triageEntry, agents []panel.AgentRow) []triageFamily {
	byID := xslice.KeyBy(agents, func(item panel.AgentRow) string { return item.Agent.ID })
	at := make(map[string]int)
	var families []triageFamily
	for _, item := range agents {
		entry, ok := entries[item.Agent.ID]
		if !ok {
			continue
		}

		head := findTriageHead(item, byID)
		i, seen := at[head.Agent.ID]
		if !seen {
			at[head.Agent.ID] = len(families)
			families = append(families, triageFamily{head: head, entry: entry})

			continue
		}

		if compareUrgency(entry, families[i].entry) < 0 {
			families[i].entry = entry
		}
	}

	return families
}

// findTriageHead is the agent a family in triage is led by: its parent when the parent is listed.
func findTriageHead(item panel.AgentRow, byID map[string]panel.AgentRow) panel.AgentRow {
	if parentID := item.Agent.ParentAgentID; parentID != nil {
		if parent, ok := byID[*parentID]; ok {
			return parent
		}
	}

	return item
}

// findTriageRule is the first rule an agent meets.
func findTriageRule(subject triageSubject) (triageRule, bool) {
	for _, rule := range triageRules {
		if rule.holds(subject) {
			return rule, true
		}
	}

	return triageRule{}, false
}

// holds reports whether the rule puts the agent in triage.
func (r triageRule) holds(subject triageSubject) bool {
	if r.kind == kindAgent && isAgentOnIt(subject.item) {
		return false
	}

	return r.is(subject)
}

// startedAt is when the wait behind the rule began for the agent.
func (r triageRule) startedAt(item panel.AgentRow, now time.Time) time.Time {
	if r.clock == clockState {
		return item.Agent.StateChangedAt
	}

	return now
}

// isAgentOnIt reports whether the agent is working, or holds mail that will set it working.
func isAgentOnIt(item panel.AgentRow) bool {
	return displayState(item) == panel.StateThinking || (item.Alive && item.UnreadMail)
}

// isFinishedImplementer reports whether a top-level implementer has finished its turn with nothing
// under it still working.
func isFinishedImplementer(s triageSubject) bool {
	agent := s.item.Agent

	return s.item.Alive &&
		displayState(s.item) == panel.StateIdle &&
		agent.Role == panel.RoleImplementer &&
		agent.ParentAgentID == nil &&
		!s.hasThinkingChild
}

// findThinkingParents is the ids of the agents with a child at work.
func findThinkingParents(agents []panel.AgentRow) map[string]bool {
	parents := make(map[string]bool)
	for _, item := range agents {
		if item.Agent.ParentAgentID != nil && displayState(item) == panel.StateThinking {
			parents[*item.Agent.ParentAgentID] = true
		}
	}

	return parents
}

// agentsLayout is the agents list split into triage and the tree under it.
type agentsLayout struct {
	zone []agentRow
	// waiting is how many families in triage the zone has no room for.
	waiting int
	tree    []agentRow
}

// cursorRows are the rows the keyboard can reach.
func (l agentsLayout) cursorRows() []agentRow {
	return slices.Concat(l.zone, l.tree)
}

// splitTriage takes the most urgent families in triage the zone has room for out of the listing,
// each head with its children under it and the snoozed families last.
func splitTriage(z triageZone, s panelShared, showHidden bool) (zone []agentRow, waiting int, rest []panel.AgentRow) {
	children := make(map[string][]panel.AgentRow)
	for _, item := range s.listing.agents {
		if parentID := item.Agent.ParentAgentID; parentID != nil {
			children[*parentID] = append(children[*parentID], item)
		}
	}

	families := xslice.Filter(findTriageFamilies(z.entries, s.listing.agents), func(f triageFamily) bool {
		return !f.head.Agent.Hidden || showHidden
	})
	slotted := z.slotFamilies(families)
	inZone := make(map[string]bool)
	workflows := xslice.KeyBy(s.listing.workflows, func(w panel.Workflow) string { return w.ID })
	for _, family := range slotted {
		head := family.head
		snoozed := z.isSnoozed(head.Agent.ID)
		inZone[head.Agent.ID] = true
		zone = append(zone, agentRow{
			item:     head,
			workflow: workflows[head.Agent.WorkflowID],
			snoozed:  snoozed,
		})
		headRow := len(zone) - 1
		for _, child := range children[head.Agent.ID] {
			inZone[child.Agent.ID] = true
			if child.Agent.Hidden {
				zone[headRow].hiddenCount++
				if !showHidden {
					continue
				}
			}

			zone = append(zone, agentRow{
				item:     child,
				workflow: workflows[child.Agent.WorkflowID],
				child:    true,
				snoozed:  snoozed,
			})
		}
	}

	rest = xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool { return !inZone[item.Agent.ID] })

	return zone, len(families) - len(slotted), rest
}

// slotFamilies is the families the zone draws, in the order it was frozen in while it is frozen.
func (z triageZone) slotFamilies(families []triageFamily) []triageFamily {
	if z.frozen != nil {
		rank := make(map[string]int, len(z.frozen))
		for i, headID := range z.frozen {
			rank[headID] = i
		}

		slotted := xslice.Filter(families, func(f triageFamily) bool {
			_, kept := rank[f.head.Agent.ID]

			return kept
		})
		slices.SortFunc(slotted, func(a, b triageFamily) int { return cmp.Compare(rank[a.head.Agent.ID], rank[b.head.Agent.ID]) })

		return slotted
	}

	slices.SortFunc(families, func(a, b triageFamily) int {
		return cmp.Or(
			compareBools(z.isSnoozed(a.head.Agent.ID), z.isSnoozed(b.head.Agent.ID)),
			compareUrgency(a.entry, b.entry),
			cmp.Compare(a.head.Agent.ID, b.head.Agent.ID),
		)
	})

	return families[:min(z.capacity, len(families))]
}

// compareBools orders false ahead of true.
func compareBools(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	default:
		return 1
	}
}

// compareUrgency orders two entries in triage, the more urgent first.
func compareUrgency(a, b triageEntry) int {
	return cmp.Or(cmp.Compare(a.rule.tier, b.rule.tier), a.since.Compare(b.since))
}

// settleTriage freezes the zone and holds the family the cursor is on while the agents list has the
// cursor on the zone, whether the keys are in the panel or in that agent's pane, and thaws it the
// moment the cursor leaves, putting the cursor back on the row it was on.
func (m model) settleTriage() model {
	var zone []agentRow
	if m.screen == screenAgents && len(m.agents.triage.entries) > 0 {
		zone = m.agents.layout(m.shared()).zone
	}

	reading := m.agents.cursor < len(zone)
	m.agents.triage.held = ""
	if reading {
		m.agents.triage.held = findZoneHead(zone, m.agents.cursor).item.Agent.ID
	}

	switch {
	case reading && m.agents.triage.frozen == nil:
		m.agents.triage.frozen = []string{}
		for _, row := range zone {
			if !row.child {
				m.agents.triage.frozen = append(m.agents.triage.frozen, row.item.Agent.ID)
			}
		}
	case !reading && m.agents.triage.frozen != nil:
		hovered := m.agents.list().hovered(m.shared())
		m.agents.triage.frozen = nil
		m.agents.apply(m.shared(), hovered)
	}

	return m
}

// findZoneHead is the head of the family the zone row at the index belongs to.
func findZoneHead(zone []agentRow, idx int) agentRow {
	for i := idx; i > 0; i-- {
		if !zone[i].child {
			return zone[i]
		}
	}

	return zone[0]
}

// findZoneSlot is which family in the zone the cursor is on, counting from the top, or -1 off the zone.
func (a *agentsScreen) findZoneSlot(s panelShared) int {
	slot := -1
	for i, row := range a.layout(s).zone {
		if !row.child {
			slot++
		}
		if i == a.cursor {
			return slot
		}
	}

	return -1
}

// landInTriage moves the cursor off a zone row that has left the list onto the family now in its
// slot, or into the tree once the zone is empty.
func (a *agentsScreen) landInTriage(s panelShared, selected string, slot int) {
	rows := a.rows(s)
	if slot < 0 || rowIndexOfKey(rows, selected) >= 0 {
		return
	}

	var heads []int
	for i, row := range a.layout(s).zone {
		if !row.child {
			heads = append(heads, i)
		}
	}

	if len(heads) == 0 {
		a.cursor = firstAgentRowIndex(rows)

		return
	}

	a.cursor = heads[min(slot, len(heads)-1)]
}

// jumpToTriage puts the cursor on the zone's first agent.
func (a *agentsScreen) jumpToTriage(h navHost, s panelShared) {
	if len(a.layout(s).zone) == 0 {
		h.note(messageInfo, "nothing to triage")

		return
	}

	a.cursor = 0
	h.stageSelection()
}

// triageSnapshot is the zone as the agents list draws it.
type triageSnapshot struct {
	zone    []agentRow
	waiting int
}

// triageRows are the lines the zone draws ahead of the tree, none while it is empty.
func triageRows(params snapshotParams) []view.Row {
	queue := params.triage
	if len(queue.zone) == 0 {
		return nil
	}

	var rows []view.Row
	for _, r := range queue.zone {
		row := params.renaming.drawnOver(snapshotRow(r, params))
		if !r.child {
			row.Kind = view.KindAgent
		}
		if r.item.Agent.Role == panel.RoleOrchestrator {
			row.Name = r.workflow.Name
		}
		row.Faded = r.snoozed
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

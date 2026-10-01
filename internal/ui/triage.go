package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

const (
	triageMoreKey    = "triage:more"
	triagePadKey     = "triage:pad:"
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
	// capacity is how many rows the zone draws, and zero turns it off.
	capacity int
	// entries are the agents in triage by id.
	entries map[string]triageEntry
	// snoozed are the families taken out of the zone, by head id, until what they wait on changes.
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
	byID := keyAgentsByID(agents)
	families := z.familiesByHead(agents)
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

// isInTriage reports whether the agent is in triage.
func (z *triageZone) isInTriage(id string) bool {
	_, ok := z.entries[id]

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
	} else {
		z.snooze(family)
	}

	z.refreeze(agents)

	return true
}

// refreeze takes the snoozed families out of the frozen zone and lets in the families waiting for a
// slot.
func (z *triageZone) refreeze(agents []panel.AgentRow) {
	if z.frozen == nil {
		return
	}

	frozen := xslice.Filter(z.frozen, func(id string) bool { return !z.isSnoozed(id) })
	waiting := xslice.Filter(slices.Collect(maps.Values(z.familiesByHead(agents))), func(f triageFamily) bool {
		return !z.isSnoozed(f.head.Agent.ID) && !slices.Contains(frozen, f.head.Agent.ID)
	})
	slices.SortFunc(waiting, z.compareFamilies)
	z.frozen = slices.Concat(frozen, xslice.Map(waiting, func(f triageFamily) string { return f.head.Agent.ID }))
}

// seeFinished snoozes the agent's family when it waits only to be looked at.
func (z *triageZone) seeFinished(item panel.AgentRow, agents []panel.AgentRow) {
	family, ok := z.findFamily(item, agents)
	if ok && family.entry.rule.tier == tierFinished {
		z.snooze(family)
	}
}

// snooze takes the family out of the zone until what it waits on changes.
func (z *triageZone) snooze(family triageFamily) {
	if z.snoozed == nil {
		z.snoozed = make(map[string]triageSnooze)
	}

	z.snoozed[family.head.Agent.ID] = triageSnooze{rule: family.entry.rule.name, began: family.entry.began}
}

// findFamily is the family in triage the agent belongs to.
func (z *triageZone) findFamily(item panel.AgentRow, agents []panel.AgentRow) (triageFamily, bool) {
	head := findTriageHead(item, keyAgentsByID(agents))
	family, ok := z.familiesByHead(agents)[head.Agent.ID]

	return family, ok
}

func (z *triageZone) familiesByHead(agents []panel.AgentRow) map[string]triageFamily {
	return xslice.KeyBy(findTriageFamilies(z.entries, agents), func(f triageFamily) string { return f.head.Agent.ID })
}

// findTriageFamilies groups the agents in triage under their heads, in the listing's order.
func findTriageFamilies(entries map[string]triageEntry, agents []panel.AgentRow) []triageFamily {
	byID := keyAgentsByID(agents)
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

// isAgentOnIt reports whether the agent is working, or is owed something that will set it working.
func isAgentOnIt(item panel.AgentRow) bool {
	//nolint:godox,fussy // follow-ups kept beside the rule they would change
	// TODO(@harrisoncramer): A check that never finishes, like a required check that never
	// reports, keeps the agent out of triage until something else on its pull request changes.
	//
	// TODO(@harrisoncramer): An agent that keeps failing its checks is mailed, pushes and waits
	// again each time, so a person never sees it however many rounds it burns.
	//
	// TODO(@harrisoncramer): Mail the agent never reads keeps it out of triage for as long as it
	// sits there unread.
	if displayState(item) == panel.StateThinking {
		return true
	}

	return item.Alive && (item.UnreadMail || isAwaitingChecks(item.PullRequest))
}

// isAwaitingChecks reports whether an open pull request has checks still running on its head.
func isAwaitingChecks(pullRequest panel.PullRequestStanding) bool {
	return pullRequest.Checks == pullrequestv1.Checks_CHECKS_PENDING && isPullRequestOpen(pullRequest.State)
}

func isPullRequestOpen(state pullrequestv1.PullRequestState) bool {
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING:
		return true
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
	}

	return false
}

// isFinishedImplementer reports whether a top-level implementer has finished its turn with nothing
// under it still working and no reviewer asked to look again.
func isFinishedImplementer(s triageSubject) bool {
	agent := s.item.Agent

	return s.item.Alive &&
		displayState(s.item) == panel.StateIdle &&
		agent.Role == panel.RoleImplementer &&
		agent.ParentAgentID == nil &&
		!s.hasThinkingChild &&
		s.item.PullRequest.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED
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

// buildZone draws the most urgent families in triage the zone has room for, leaving the snoozed out,
// and counts the families still waiting for a slot.
func buildZone(z triageZone, m *model, showHidden bool) (zone []agentRow, waiting int) {
	children := childrenByParent(m.listing.agents)
	families := xslice.Filter(findTriageFamilies(z.entries, m.listing.agents), func(f triageFamily) bool {
		return (!f.head.Agent.Hidden || showHidden) && !z.isSnoozed(f.head.Agent.ID)
	})
	var slotted []triageSlot
	used := 0
	for _, family := range z.orderFamilies(families) {
		rows := buildFamilyRows(family.head, children[family.head.Agent.ID], showHidden)
		asking := z.countAsking(rows[1:])
		if used >= z.capacity || (len(slotted) > 0 && used+1+asking > z.capacity) {
			break
		}

		slotted = append(slotted, triageSlot{family: family, rows: rows, asking: asking})
		used += 1 + asking
	}

	spare := z.capacity - used
	workflows := xslice.KeyBy(m.listing.workflows, func(w panel.Workflow) string { return w.ID })
	for _, slot := range slotted {
		drawn := z.pickChildren(slot.rows[1:], slot.asking+spare)
		spare -= len(drawn) - slot.asking
		for _, row := range slot.rows {
			if row.child && !drawn[row.item.Agent.ID] {
				continue
			}

			row.workflow = workflows[row.item.Agent.WorkflowID]
			row.triage = true
			zone = append(zone, row)
		}
	}

	return zone, len(families) - len(slotted)
}

// triageSlot is a family the zone draws, with its rows and how many of its children are in triage.
type triageSlot struct {
	family triageFamily
	rows   []agentRow
	asking int
}

// countAsking is how many of a family's child rows are in triage.
func (z triageZone) countAsking(rows []agentRow) int {
	return len(xslice.Filter(rows, func(row agentRow) bool { return z.isInTriage(row.item.Agent.ID) }))
}

// pickChildren is the ids of as many of a family's child rows as fit in room, those in triage first.
func (z triageZone) pickChildren(rows []agentRow, room int) map[string]bool {
	ranked := slices.Clone(rows)
	slices.SortStableFunc(ranked, func(a, b agentRow) int {
		return compareBools(!z.isInTriage(a.item.Agent.ID), !z.isInTriage(b.item.Agent.ID))
	})

	picked := make(map[string]bool)
	for _, row := range ranked[:min(len(ranked), room)] {
		picked[row.item.Agent.ID] = true
	}

	return picked
}

// orderFamilies is the families in the order the zone draws them, the order it was frozen in while
// it is frozen.
func (z triageZone) orderFamilies(families []triageFamily) []triageFamily {
	if z.frozen != nil {
		rank := make(map[string]int, len(z.frozen))
		for i, headID := range z.frozen {
			rank[headID] = i
		}

		kept := xslice.Filter(families, func(f triageFamily) bool {
			_, ok := rank[f.head.Agent.ID]

			return ok
		})
		slices.SortFunc(kept, func(a, b triageFamily) int { return cmp.Compare(rank[a.head.Agent.ID], rank[b.head.Agent.ID]) })

		return kept
	}

	slices.SortFunc(families, z.compareFamilies)

	return families
}

// compareFamilies orders two families in triage as the zone draws them.
func (z triageZone) compareFamilies(a, b triageFamily) int {
	return cmp.Or(
		compareUrgency(a.entry, b.entry),
		cmp.Compare(a.head.Agent.ID, b.head.Agent.ID),
	)
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
func (m *model) settleTriage() {
	var zone []agentRow
	if m.screen == screenAgents && len(m.agents.triage.entries) > 0 {
		zone = m.agents.layout(m).zone
	}

	reading := m.agents.cursor < len(zone)
	m.agents.triage.held = ""
	if reading {
		m.agents.triage.held = findZoneHead(zone, m.agents.cursor).item.Agent.ID
	}

	switch {
	case reading && m.agents.triage.frozen == nil:
		m.agents.triage.frozen = xslice.Map(zoneHeads(zone), func(i int) string { return zone[i].item.Agent.ID })
	case !reading && m.agents.triage.frozen != nil:
		hovered := m.agents.list().hovered(m)
		m.agents.triage.frozen = nil
		m.agents.apply(m, hovered)
	}
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

func zoneHeads(zone []agentRow) []int {
	var heads []int
	for i, row := range zone {
		if !row.child {
			heads = append(heads, i)
		}
	}

	return heads
}

// findZoneSlot is which family in the zone the cursor is on, counting from the top, or -1 off the zone.
func (a *agentsScreen) findZoneSlot(m *model) int {
	zone := a.layout(m).zone
	if a.cursor < 0 || a.cursor >= len(zone) {
		return -1
	}

	slot := -1
	for _, head := range zoneHeads(zone) {
		if head > a.cursor {
			break
		}
		slot++
	}

	return slot
}

// moveAfterSnooze puts the cursor on the family above the zone's slot, on the top family when the
// slot was the top, or on the first agent of the list once the zone is empty.
func (a *agentsScreen) moveAfterSnooze(m *model, slot int) {
	heads := zoneHeads(a.layout(m).zone)
	if len(heads) == 0 {
		m.note(messageInfo, "nothing left to triage")
		a.cursor = firstAgentRowIndex(a.rows(m))

		return
	}

	a.cursor = heads[min(max(slot-1, 0), len(heads)-1)]
}

// landInTriage moves the cursor off a zone row that has left the list onto the family now in its
// slot, or into the tree once the zone is empty.
func (a *agentsScreen) landInTriage(m *model, selected string, slot int) {
	rows := a.rows(m)
	if slot < 0 || rowIndexOfKey(rows, selected) >= 0 {
		return
	}

	heads := zoneHeads(a.layout(m).zone)
	if len(heads) == 0 {
		a.cursor = firstAgentRowIndex(rows)

		return
	}

	a.cursor = heads[min(slot, len(heads)-1)]
}

// jumpToTriage puts the cursor on the zone's first agent.
func (a *agentsScreen) jumpToTriage(m *model) {
	if len(a.layout(m).zone) == 0 {
		m.note(messageInfo, "nothing to triage")

		return
	}

	a.cursor = 0
	m.stageSelection()
}

// triageSnapshot is the zone as the agents list draws it.
type triageSnapshot struct {
	// height is how many rows of agents the zone draws, zero when it is off.
	height  int
	zone    []agentRow
	waiting int
}

// triageRows are the lines the zone draws ahead of the tree, the same number whether it is full or
// empty.
func triageRows(params snapshotParams) []view.Row {
	queue := params.triage
	if queue.height == 0 {
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
		if !r.child && r.item.Agent.Role == panel.RoleImplementer {
			row.Workflow = r.workflow.Name
		}
		rows = append(rows, row)
	}

	for i := len(rows); i < queue.height; i++ {
		name := ""
		if i == 0 {
			name = "nothing to triage"
		}

		rows = append(rows, newTriageNote(fmt.Sprintf("%s%d", triagePadKey, i), name))
	}

	status := ""
	if queue.waiting > 0 {
		status = fmt.Sprintf("+%d more waiting for a slot", queue.waiting)
	}

	return append(rows,
		newTriageNote(triageMoreKey, status),
		view.Row{Key: triageDividerKey, Kind: view.KindDivider},
	)
}

// newTriageNote is a faded line of the zone that names no agent.
func newTriageNote(key, name string) view.Row {
	return view.Row{
		Key:     key,
		Kind:    view.KindTriageMore,
		Name:    name,
		Process: view.ProcessNone,
		Faded:   true,
	}
}

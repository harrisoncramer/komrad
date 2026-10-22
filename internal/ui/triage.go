package ui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

const (
	triageMoreKey    = "triage:more"
	triagePadKey     = "triage:pad:"
	triageHeaderKey  = "triage:header"
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
	item                panel.AgentRow
	task                panel.Task
	hasThinkingReviewer bool
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
			return s.task.Sequence.IsInProgress() && s.item.Agent.Role == panel.RoleWriter
		},
	},
	{
		name:  "finished",
		tier:  tierFinished,
		kind:  kindAgent,
		clock: clockState,
		is:    isFinishedWriter,
	},
}

// triageZone is the agents waiting on a person, rendered ahead of the tree while it has room for any.
type triageZone struct {
	// capacity is how many tasks the zone renders, and zero turns it off.
	capacity int
	// entries are the agents in triage by id.
	entries map[string]triageEntry
	// snoozed are the tasks taken out of the zone, by triageKey, until what they wait on changes.
	snoozed map[string]triageSnooze
	// frozen is the order of the heads rendered while a person reads the zone, which holds still until
	// they leave it, and nil when it is free to move.
	frozen []string
	// held is the head of the task the cursor is on, which keeps its place in the zone after it stops
	// needing triage until the cursor moves to another task.
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

// triageSnooze is the reason a task was snoozed on, read off its most urgent entry.
type triageSnooze struct {
	headID string
	rule   string
	began  time.Time
}

// triageTask is a task in triage led by its writer, or an agent on no task, named by the most
// urgent entry among them.
type triageTask struct {
	head  panel.AgentRow
	entry triageEntry
}

// isOn reports whether the zone is rendered at all.
func (z *triageZone) isOn() bool {
	return z.capacity > 0
}

// refresh reads which agents need triage from the listing, timing new waits from now.
func (z *triageZone) refresh(agents []panel.AgentRow, tasks panel.Tasks, now time.Time) {
	if !z.isOn() {
		z.entries = nil
		z.snoozed = nil

		return
	}

	writers := panel.WritersByTask(agents)
	thinkingWriters := findThinkingWriters(agents, writers)
	entries := make(map[string]triageEntry)
	for _, item := range agents {
		id := item.Agent.ID
		if tasks.IsHidden(item.Agent) || item.Agent.HeldAt != nil {
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

		rule, ok := findTriageRule(triageSubject{
			item:                item,
			task:                tasks.FindForAgent(item.Agent),
			hasThinkingReviewer: thinkingWriters[id],
		})
		if !ok {
			if wasQueued && z.isHeld(item, writers) {
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

		entries[id] = triageEntry{
			since: since,
			began: began,
			rule:  rule,
		}
	}

	z.entries = entries
	z.pruneSnoozes(agents, tasks)
}

// isHeld reports whether the agent is on the task the cursor is on.
func (z *triageZone) isHeld(item panel.AgentRow, writers map[string]panel.AgentRow) bool {
	return z.held != "" && findTriageHead(item, writers).Agent.ID == z.held
}

// pruneSnoozes ends every snooze whose task has left triage or now waits on something else.
func (z *triageZone) pruneSnoozes(agents []panel.AgentRow, tasks panel.Tasks) {
	byID := keyAgentsByID(agents)
	allTriaged := z.tasksByKey(agents)
	for key, snooze := range z.snoozed {
		if tasks.IsHidden(byID[snooze.headID].Agent) {
			continue
		}

		triaged, ok := allTriaged[key]
		if !ok || !snooze.isFor(triaged.entry) {
			delete(z.snoozed, key)
		}
	}
}

// isFor reports whether the snooze still stands for what the task waits on.
func (s triageSnooze) isFor(entry triageEntry) bool {
	return s.rule == entry.rule.name && s.began.Equal(entry.began)
}

// isSnoozed reports whether the task, or the agent standing alone, under the key is snoozed.
func (z *triageZone) isSnoozed(key string) bool {
	_, ok := z.snoozed[key]

	return ok
}

// toggleSnooze snoozes the task under the key in triage, or wakes it when it is snoozed,
// reporting false when the task is not in triage.
func (z *triageZone) toggleSnooze(key string, agents []panel.AgentRow) bool {
	triaged, ok := z.tasksByKey(agents)[key]
	if !ok {
		return false
	}

	if z.isSnoozed(key) {
		delete(z.snoozed, key)
	} else {
		z.snooze(triaged)
	}

	z.refreeze(agents)

	return true
}

// refreeze takes the snoozed tasks out of the frozen zone and lets in the tasks waiting for a
// slot.
func (z *triageZone) refreeze(agents []panel.AgentRow) {
	if z.frozen == nil {
		return
	}

	byID := keyAgentsByID(agents)
	frozen := xslice.Filter(z.frozen, func(id string) bool { return !z.isSnoozed(triageKey(byID[id])) })
	waiting := xslice.Filter(findTriageTasks(z.entries, agents), func(triaged triageTask) bool {
		return !z.isSnoozed(triageKey(triaged.head)) && !slices.Contains(frozen, triaged.head.Agent.ID)
	})
	slices.SortFunc(waiting, z.compareTasks)
	z.frozen = slices.Concat(frozen, xslice.Map(waiting, func(triaged triageTask) string { return triaged.head.Agent.ID }))
}

// seeFinished snoozes the agent's task when it waits only to be looked at.
func (z *triageZone) seeFinished(item panel.AgentRow, agents []panel.AgentRow) {
	triaged, ok := z.findTask(item, agents)
	if ok && triaged.entry.rule.tier == tierFinished {
		z.snooze(triaged)
	}
}

// snooze takes the task out of the zone until what it waits on changes.
func (z *triageZone) snooze(triaged triageTask) {
	if z.snoozed == nil {
		z.snoozed = make(map[string]triageSnooze)
	}

	z.snoozed[triageKey(triaged.head)] = triageSnooze{
		headID: triaged.head.Agent.ID,
		rule:   triaged.entry.rule.name,
		began:  triaged.entry.began,
	}
}

// findTask is the agent's task in triage.
func (z *triageZone) findTask(item panel.AgentRow, agents []panel.AgentRow) (triageTask, bool) {
	head := findTriageHead(item, panel.WritersByTask(agents))
	triaged, ok := z.tasksByKey(agents)[triageKey(head)]

	return triaged, ok
}

// tasksByKey is the most urgent entry in triage on each task, or of each agent standing alone.
func (z *triageZone) tasksByKey(agents []panel.AgentRow) map[string]triageTask {
	byKey := make(map[string]triageTask)
	for _, triaged := range findTriageTasks(z.entries, agents) {
		key := triageKey(triaged.head)
		if was, ok := byKey[key]; ok && compareUrgency(was.entry, triaged.entry) <= 0 {
			continue
		}
		byKey[key] = triaged
	}

	return byKey
}

// triageKey is what a snooze of the head's task is kept under: its task, or the head itself when
// it is on none.
func triageKey(head panel.AgentRow) string {
	if head.Agent.TaskID != "" {
		return head.Agent.TaskID
	}

	return head.Agent.ID
}

// findTriageTasks groups the agents in triage by task, under their heads, in the listing's order.
func findTriageTasks(entries map[string]triageEntry, agents []panel.AgentRow) []triageTask {
	writers := panel.WritersByTask(agents)
	at := make(map[string]int)
	var allTriaged []triageTask
	for _, item := range agents {
		entry, ok := entries[item.Agent.ID]
		if !ok {
			continue
		}

		head := findTriageHead(item, writers)
		i, seen := at[head.Agent.ID]
		if !seen {
			at[head.Agent.ID] = len(allTriaged)
			allTriaged = append(allTriaged, triageTask{head: head, entry: entry})

			continue
		}

		if compareUrgency(entry, allTriaged[i].entry) < 0 {
			allTriaged[i].entry = entry
		}
	}

	return allTriaged
}

// findTriageHead is the agent a task in triage is led by: its task's writer when the writer is listed.
func findTriageHead(item panel.AgentRow, writers map[string]panel.AgentRow) panel.AgentRow {
	if writer, ok := panel.FindWriter(item, writers); ok {
		return writer
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
	if r.kind == kindAgent && isAgentOnIt(subject.item, subject.task) {
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
func isAgentOnIt(item panel.AgentRow, task panel.Task) bool {
	//nolint:godox,fussy // follow-ups kept beside the rule they would change
	// TODO(@harrisoncramer): A check that never finishes, like a required check that never
	// reports, keeps the agent out of triage until something else on its pull request changes.
	//
	// TODO(@harrisoncramer): An agent that keeps failing its checks is mailed, pushes and waits
	// again each time, so a person never sees it however many snapshots it burns.
	//
	// TODO(@harrisoncramer): Mail the agent never reads keeps it out of triage for as long as it
	// sits there unread.
	if displayState(item) == panel.StateThinking {
		return true
	}

	return item.Alive && (item.UnreadMail || isAwaitingChecks(task.PullRequest))
}

// isAwaitingChecks reports whether an open pull request has checks still running on its head,
// failed ones among them or not.
func isAwaitingChecks(pullRequest panel.PullRequestStanding) bool {
	checks := pullRequest.MergeReadiness.Checks
	running := checks == pullrequestv1.Checks_CHECKS_PENDING || checks == pullrequestv1.Checks_CHECKS_FAILING_RUNNING

	return running && isPullRequestOpen(pullRequest.State)
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

// isFinishedWriter reports whether a writer has finished its turn with nothing under it still
// working and no reviewer asked to look again.
func isFinishedWriter(s triageSubject) bool {
	agent := s.item.Agent

	return s.item.Alive &&
		displayState(s.item) == panel.StateIdle &&
		agent.Role == panel.RoleWriter &&
		!s.hasThinkingReviewer &&
		s.task.PullRequest.State != pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED
}

// findThinkingWriters is the ids of the writers with a reviewer on their task at work.
func findThinkingWriters(agents []panel.AgentRow, writers map[string]panel.AgentRow) map[string]bool {
	thinking := make(map[string]bool)
	for _, item := range agents {
		if writer, ok := panel.FindWriter(item, writers); ok && displayState(item) == panel.StateThinking {
			thinking[writer.Agent.ID] = true
		}
	}

	return thinking
}

// agentsLayout is the agents list split into triage and the tree under it.
type agentsLayout struct {
	zone []agentRow
	// waiting is how many tasks in triage the zone has no room for.
	waiting int
	tree    []agentRow
	all     []agentRow
}

// cursorRows are the rows the keyboard can reach.
func (l agentsLayout) cursorRows() []agentRow {
	return l.all
}

func (l agentsLayout) cursorRow(cursor int) (agentRow, bool) {
	if cursor < len(l.zone) {
		return list.At(l.zone, cursor)
	}

	return list.At(l.tree, cursor-len(l.zone))
}

// buildZone renders the most urgent tasks the zone has slots for and counts those left waiting.
func buildZone(z triageZone, m *model, showHidden bool) (zone []agentRow, waiting int) {
	reviewers := m.listing.Reviewers
	allTriaged := xslice.Filter(findTriageTasks(z.entries, m.listing.Agents), func(triaged triageTask) bool {
		return (!m.listing.Tasks.IsHidden(triaged.head.Agent) || showHidden) && !z.isSnoozed(triageKey(triaged.head))
	})
	ordered := z.orderTasks(allTriaged)
	slotted := ordered[:min(len(ordered), z.capacity)]
	workflows := xslice.KeyBy(m.listing.Workflows, func(w panel.Workflow) string { return w.ID })
	for _, triaged := range slotted {
		for _, row := range buildAgentRows(triaged.head, reviewers[triaged.head.Agent.ID]) {
			row.workflow = workflows[row.item.Agent.WorkflowID]
			row.triage = true
			zone = append(zone, row)
		}
	}

	return zone, len(allTriaged) - len(slotted)
}

// orderTasks is the tasks in the order the zone renders them, the order it was frozen in while
// it is frozen.
func (z triageZone) orderTasks(allTriaged []triageTask) []triageTask {
	if z.frozen != nil {
		rank := make(map[string]int, len(z.frozen))
		for i, headID := range z.frozen {
			rank[headID] = i
		}

		kept := xslice.Filter(allTriaged, func(triaged triageTask) bool {
			_, ok := rank[triaged.head.Agent.ID]

			return ok
		})
		slices.SortFunc(kept, func(a, b triageTask) int { return cmp.Compare(rank[a.head.Agent.ID], rank[b.head.Agent.ID]) })

		return kept
	}

	slices.SortFunc(allTriaged, z.compareTasks)

	return allTriaged
}

// compareTasks orders two tasks in triage as the zone renders them.
func (z triageZone) compareTasks(a, b triageTask) int {
	return cmp.Or(
		compareUrgency(a.entry, b.entry),
		cmp.Compare(a.head.Agent.ID, b.head.Agent.ID),
	)
}

// compareUrgency orders two entries in triage, the more urgent first.
func compareUrgency(a, b triageEntry) int {
	return cmp.Or(cmp.Compare(a.rule.tier, b.rule.tier), a.since.Compare(b.since))
}

// settleTriage freezes the zone and holds the task the cursor is on while the agents list has the
// cursor on the zone, whether the keys are in the panel or in that agent's pane, and thaws it the
// moment the cursor leaves, putting the cursor back on the row it was on.
func (m *model) settleTriage() {
	var zone []agentRow
	if m.screen == screen.Agents && len(m.agents.triage.entries) > 0 {
		zone = m.agents.layout(m).zone
	}

	reading := m.agents.Row < len(zone)
	m.agents.triage.held = ""
	if reading {
		m.agents.triage.held = findZoneHead(zone, m.agents.Row).item.Agent.ID
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

// findZoneHead is the head of the task the zone row at the index belongs to.
func findZoneHead(zone []agentRow, idx int) agentRow {
	for i := idx; i > 0; i-- {
		if !zone[i].underTask {
			return zone[i]
		}
	}

	return zone[0]
}

func zoneHeads(zone []agentRow) []int {
	var heads []int
	for i, row := range zone {
		if !row.underTask {
			heads = append(heads, i)
		}
	}

	return heads
}

// findZoneSlot is which task in the zone the cursor is on, counting from the top, or -1 off the zone.
func (a *agentsScreen) findZoneSlot(m *model) int {
	zone := a.layout(m).zone
	if a.Row < 0 || a.Row >= len(zone) {
		return -1
	}

	slot := -1
	for _, head := range zoneHeads(zone) {
		if head > a.Row {
			break
		}
		slot++
	}

	return slot
}

// moveAfterSnooze puts the cursor on the task above the zone's slot, on the top task when the
// slot was the top, or on the first agent of the list once the zone is empty.
func (a *agentsScreen) moveAfterSnooze(m *model, slot int) {
	heads := zoneHeads(a.layout(m).zone)
	if len(heads) == 0 {
		m.note(messageInfo, "nothing left to triage")
		a.Row = firstAgentRowIndex(a.rows(m))

		return
	}

	a.Row = heads[min(max(slot-1, 0), len(heads)-1)]
}

// landInTriage moves the cursor off a zone row that has left the list onto the task now in its
// slot, or into the tree once the zone is empty.
func (a *agentsScreen) landInTriage(m *model, selected string, slot int) {
	rows := a.rows(m)
	if slot < 0 || list.IndexOf(rows, selected) >= 0 {
		return
	}

	heads := zoneHeads(a.layout(m).zone)
	if len(heads) == 0 {
		a.Row = firstAgentRowIndex(rows)

		return
	}

	a.Row = heads[min(slot, len(heads)-1)]
}

// jumpToTriage puts the cursor on the zone's first agent.
func (a *agentsScreen) jumpToTriage(m *model) {
	if len(a.layout(m).zone) == 0 {
		m.note(messageInfo, "nothing to triage")

		return
	}

	a.Row = 0
	m.stageSelection()
}

// triageSnapshot is the zone as the agents list renders it.
type triageSnapshot struct {
	// height is the fewest rows of agents the zone renders, zero when it is off.
	height  int
	zone    []agentRow
	waiting int
}

// triageRows are the lines the zone renders ahead of the tree, padded to its height when it holds fewer.
func triageRows(params snapshotParams) []view.Row {
	queue := params.triage
	if queue.height == 0 {
		return nil
	}

	var rows []view.Row
	for _, r := range queue.zone {
		row := params.renaming.RenderedOver(snapshotRow(r, params))
		row.Kind = view.KindAgent
		if r.item.Agent.Role == panel.RoleOrchestrator {
			row.Name = r.workflow.Name
		}
		if !r.underTask && r.item.Agent.Role == panel.RoleWriter {
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

	header := view.Row{
		Key:  triageHeaderKey,
		Kind: view.KindDivider,
		Name: "Triage",
	}

	return append(append([]view.Row{header}, rows...),
		newTriageNote(triageMoreKey, status),
		view.Row{
			Key:  triageDividerKey,
			Kind: view.KindDivider,
			Name: "All Agents",
		},
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

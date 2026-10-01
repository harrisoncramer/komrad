package ui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

const (
	triageMoreKey    = "triage:more"
	triageDividerKey = "triage:divider"
)

// triageTier is how urgently an agent wants a person, the most urgent first.
type triageTier int

const (
	tierBlocked triageTier = iota + 1
	tierBroken
	tierReady
	tierYourMove
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
		tier:  tierBlocked,
		kind:  kindPerson,
		clock: clockState,
		is:    func(s triageSubject) bool { return displayState(s.item) == panel.StateWaiting },
	},
	{
		name:  "checkout conflict",
		tier:  tierBlocked,
		kind:  kindAgent,
		clock: clockSeen,
		is: func(s triageSubject) bool {
			return s.item.Agent.SequenceInProgress && s.item.Agent.ParentAgentID == nil
		},
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
		name:  "ci failing",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockSeen,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING),
	},
	{
		name:  "pr conflict",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockSeen,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING),
	},
	{
		name:  "ready to merge",
		tier:  tierReady,
		kind:  kindPerson,
		clock: clockSeen,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED),
	},
	{
		name:  "ready for review",
		tier:  tierReady,
		kind:  kindPerson,
		clock: clockSeen,
		is: func(s triageSubject) bool {
			return hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN)(s) &&
				s.item.PullRequest.Checks == pullrequestv1.Checks_CHECKS_PASSING
		},
	},
	{
		name:  "approved, behind",
		tier:  tierReady,
		kind:  kindAgent,
		clock: clockSeen,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING),
	},
	{
		name:  "changes requested",
		tier:  tierReady,
		kind:  kindAgent,
		clock: clockSeen,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED),
	},
	{
		name:  "unanswered comments",
		tier:  tierReady,
		kind:  kindAgent,
		clock: clockSeen,
		is:    func(s triageSubject) bool { return s.item.PersonComments.GetOpen() > 0 },
	},
	{
		name:  "done",
		tier:  tierYourMove,
		kind:  kindAgent,
		clock: clockSeen,
		is: hasPullRequestIn(
			pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
			pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED,
		),
	},
	{
		name:  "idle",
		tier:  tierYourMove,
		kind:  kindAgent,
		clock: clockState,
		is:    isIdleImplementer,
	},
}

// triageZone is the agents waiting on a person, drawn ahead of the tree while it has room for any.
type triageZone struct {
	// capacity is how many agents the zone draws, and zero turns it off.
	capacity int
	// entries are the agents in triage by id.
	entries map[string]triageEntry
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

// refresh reads which agents need triage from the listing, timing new waits from now.
func (z *triageZone) refresh(agents []panel.AgentRow, now time.Time) {
	if !z.isOn() {
		z.entries = nil

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
			continue
		}

		since := rule.startedAt(item, now)
		if wasQueued && was.rule.tier == rule.tier {
			since = was.since
		}

		entries[id] = triageEntry{since: since, rule: rule}
	}

	z.entries = entries
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

// isIdleImplementer reports whether a top-level implementer has finished its turn with nothing
// under it still working.
func isIdleImplementer(s triageSubject) bool {
	agent := s.item.Agent

	return s.item.Alive &&
		displayState(s.item) == panel.StateIdle &&
		agent.Role == panel.RoleImplementer &&
		agent.ParentAgentID == nil &&
		!s.hasThinkingChild
}

// hasPullRequestIn is a rule that holds while a top-level agent's pull request stands in one of
// the states.
func hasPullRequestIn(states ...pullrequestv1.PullRequestState) func(triageSubject) bool {
	return func(s triageSubject) bool {
		return s.item.Agent.ParentAgentID == nil && slices.Contains(states, s.item.PullRequest.State)
	}
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
	// waiting is how many agents in triage the zone has no room for.
	waiting int
	tree    []agentRow
}

// cursorRows are the rows the keyboard can reach.
func (l agentsLayout) cursorRows() []agentRow {
	return slices.Concat(l.zone, l.tree)
}

// splitTriage takes the most urgent families in triage the zone has room for out of the listing,
// each head with its children under it.
func splitTriage(z triageZone, s panelShared, showHidden bool) (zone []agentRow, waiting int, rest []panel.AgentRow) {
	byID := xslice.KeyBy(s.listing.agents, func(item panel.AgentRow) string { return item.Agent.ID })
	headOf := func(item panel.AgentRow) panel.AgentRow {
		if parentID := item.Agent.ParentAgentID; parentID != nil {
			if parent, ok := byID[*parentID]; ok {
				return parent
			}
		}

		return item
	}

	families := make(map[string]triageEntry)
	var heads []panel.AgentRow
	for _, item := range s.listing.agents {
		entry, ok := z.entries[item.Agent.ID]
		head := headOf(item)
		if !ok || (head.Agent.Hidden && !showHidden) {
			continue
		}

		best, seen := families[head.Agent.ID]
		if !seen {
			heads = append(heads, head)
		}
		if !seen || isMoreUrgent(entry, best) {
			families[head.Agent.ID] = entry
		}
	}

	slices.SortFunc(heads, func(a, b panel.AgentRow) int {
		left, right := families[a.Agent.ID], families[b.Agent.ID]

		return cmp.Or(
			cmp.Compare(left.rule.tier, right.rule.tier),
			left.since.Compare(right.since),
			cmp.Compare(a.Agent.ID, b.Agent.ID),
		)
	})

	slotted := heads[:min(z.capacity, len(heads))]
	inZone := make(map[string]bool)
	workflows := xslice.KeyBy(s.listing.workflows, func(w panel.Workflow) string { return w.ID })
	for _, head := range slotted {
		inZone[head.Agent.ID] = true
		zone = append(zone, agentRow{item: head, workflow: workflows[head.Agent.WorkflowID]})
		for _, item := range s.listing.agents {
			isChild := item.Agent.ParentAgentID != nil && *item.Agent.ParentAgentID == head.Agent.ID
			if !isChild || (item.Agent.Hidden && !showHidden) {
				continue
			}

			inZone[item.Agent.ID] = true
			zone = append(zone, agentRow{item: item, workflow: workflows[item.Agent.WorkflowID], child: true})
		}
	}

	rest = xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool { return !inZone[item.Agent.ID] })

	return zone, len(heads) - len(slotted), rest
}

// isMoreUrgent reports whether one entry in triage comes ahead of another.
func isMoreUrgent(entry, than triageEntry) bool {
	return entry.rule.tier < than.rule.tier || (entry.rule.tier == than.rule.tier && entry.since.Before(than.since))
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

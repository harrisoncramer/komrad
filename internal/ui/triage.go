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
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

const (
	triageHeaderKey  = "triage:header"
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
	tone  paint.Tone
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
		tone:  paint.ToneWarning,
		is:    func(s triageSubject) bool { return displayState(s.item) == panel.StateWaiting },
	},
	{
		name:  "checkout conflict",
		tier:  tierBlocked,
		kind:  kindAgent,
		clock: clockSeen,
		tone:  paint.ToneWarning,
		is: func(s triageSubject) bool {
			return s.item.Agent.Sequence.IsInProgress() && s.item.Agent.ParentAgentID == nil
		},
	},
	{
		name:  "errored",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockState,
		tone:  paint.ToneDanger,
		is:    func(s triageSubject) bool { return displayState(s.item) == panel.StateErrored },
	},
	{
		name:  "launch failed",
		tier:  tierBroken,
		kind:  kindPerson,
		clock: clockState,
		tone:  paint.ToneDanger,
		is:    func(s triageSubject) bool { return s.item.Agent.State == panel.StateFailedToCreate },
	},
	{
		name:  "cleanup failed",
		tier:  tierBroken,
		kind:  kindPerson,
		clock: clockState,
		tone:  paint.ToneDanger,
		is:    func(s triageSubject) bool { return s.item.Agent.State == panel.StateFailedToCleanup },
	},
	{
		name:  "script failed",
		tier:  tierBroken,
		kind:  kindPerson,
		clock: clockSeen,
		tone:  paint.ToneDanger,
		is:    func(s triageSubject) bool { return s.item.FailedRun },
	},
	{
		name:  "ci failing",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockSeen,
		tone:  paint.ToneDanger,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING),
	},
	{
		name:  "pr conflict",
		tier:  tierBroken,
		kind:  kindAgent,
		clock: clockSeen,
		tone:  paint.ToneDanger,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING),
	},
	{
		name:  "ready to merge",
		tier:  tierReady,
		kind:  kindPerson,
		clock: clockSeen,
		tone:  paint.ToneSuccess,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED),
	},
	{
		name:  "ready for review",
		tier:  tierReady,
		kind:  kindPerson,
		clock: clockSeen,
		tone:  paint.ToneSuccess,
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
		tone:  paint.ToneSuccess,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING),
	},
	{
		name:  "changes requested",
		tier:  tierReady,
		kind:  kindAgent,
		clock: clockSeen,
		tone:  paint.ToneWarning,
		is:    hasPullRequestIn(pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED),
	},
	{
		name:  "unanswered comments",
		tier:  tierReady,
		kind:  kindAgent,
		clock: clockSeen,
		tone:  paint.ToneWarning,
		is:    func(s triageSubject) bool { return s.item.PersonComments.GetOpen() > 0 },
	},
	{
		name:  "done",
		tier:  tierYourMove,
		kind:  kindAgent,
		clock: clockSeen,
		tone:  paint.ToneMuted,
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
		tone:  paint.ToneMuted,
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

// splitTriage takes the most urgent agents in triage the zone has room for out of the listing.
func splitTriage(z triageZone, s panelShared) (zone []agentRow, waiting int, rest []panel.AgentRow) {
	queued := xslice.Filter(s.listing.agents, func(item panel.AgentRow) bool {
		_, ok := z.entries[item.Agent.ID]

		return ok
	})
	slices.SortFunc(queued, func(a, b panel.AgentRow) int {
		left, right := z.entries[a.Agent.ID], z.entries[b.Agent.ID]

		return cmp.Or(
			cmp.Compare(left.rule.tier, right.rule.tier),
			left.since.Compare(right.since),
			cmp.Compare(a.Agent.ID, b.Agent.ID),
		)
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
	zone     []agentRow
	waiting  int
	capacity int
}

// triageRows are the lines the zone draws ahead of the tree, none while it is empty.
func triageRows(params snapshotParams) []view.Row {
	queue := params.triage
	if len(queue.zone) == 0 {
		return nil
	}

	rows := []view.Row{{
		Key:    triageHeaderKey,
		Kind:   view.KindTriageHeader,
		Name:   "triage",
		Status: view.StatusWaiting,
		Note:   view.Cell{Text: fmt.Sprintf("%d/%d", len(queue.zone), queue.capacity), Tone: paint.ToneMuted},
	}}

	for _, r := range queue.zone {
		row := params.renaming.drawnOver(snapshotRow(r, params))
		row.Kind = view.KindTriage
		reason := view.Cell{Text: r.triage.name, Tone: r.triage.tone}
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

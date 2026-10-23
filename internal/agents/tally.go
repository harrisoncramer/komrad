package agents

import (
	"maps"
	"sync"
	"time"

	agentstore "komrad/internal/store/agent"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
)

const unrecognizedHoldLimit = 5

const reconcileShellGrace = 10 * time.Second

// listingsBeforePaneGone is how many pane listings in a row have to leave a pane out before
// komrad believes it gone.
const listingsBeforePaneGone = 3

// turnEndGrace is how long after a pass first sees a turn end it may hurry an idle frame along.
const turnEndGrace = 5 * time.Second

// tally is what the reconcile passes have accrued about each agent, all of it counters and
// timestamps the panes drove.
type tally struct {
	// mu guards hasPassed, tracked and every field of the entries inside it, which the reconcile
	// pass and the RPC handler both reach.
	mu      sync.Mutex
	tracked map[uuid.UUID]trackedAgent
	// hasPassed is whether a pass has finished, so a turn end the first pass finds counts as old.
	hasPassed bool
	now       func() time.Time
}

// trackedAgent is what one pass leaves the next to work from about a single agent.
type trackedAgent struct {
	misses int
	// missesToward is the state the current run of misses is evidence for.
	missesToward      agentv1.AgentState
	listingsMissing   int
	shellSince        time.Time
	turnEnd           time.Time
	turnEndSeen       time.Time
	frameHash         uint64
	frameHashSeen     bool
	unrecognizedHolds int
}

// newTally builds an empty tally over the wall clock.
func newTally() *tally {
	return &tally{tracked: map[uuid.UUID]trackedAgent{}, now: time.Now}
}

// read is what earlier passes accrued about an agent, the zero value standing for an agent
// with no record yet.
func (t *tally) read(agentID uuid.UUID) trackedAgent {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.tracked[agentID]
}

// update hands fn what earlier passes accrued about an agent and keeps whatever it leaves,
// starting an entry when there is none.
func (t *tally) update(agentID uuid.UUID, fn func(*trackedAgent)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.tracked[agentID]
	fn(&entry)
	t.tracked[agentID] = entry
}

// updateExisting is update for the clears, which write nothing for an agent no pass has recorded
// anything about.
func (t *tally) updateExisting(agentID uuid.UUID, fn func(*trackedAgent)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.tracked[agentID]
	if !ok {
		return
	}

	fn(&entry)
	t.tracked[agentID] = entry
}

// observeTurnEnd notes when a pass first saw the turn end on agent's row.
func (t *tally) observeTurnEnd(agent agentstore.Agent) {
	if agent.Turn.EndedAt == nil {
		return
	}

	now := t.now()
	t.update(agent.ID, func(entry *trackedAgent) {
		if entry.turnEnd.Equal(*agent.Turn.EndedAt) {
			return
		}
		entry.turnEnd = *agent.Turn.EndedAt
		entry.turnEndSeen = time.Time{}
		if t.hasPassed {
			entry.turnEndSeen = now
		}
	})
}

func (t *tally) hasFreshTurnEnd(agent agentstore.Agent) bool {
	if agent.Turn.EndedAt == nil || !agent.Turn.EndedAt.After(agent.StateChangedAt) {
		return false
	}

	entry := t.read(agent.ID)

	return entry.turnEnd.Equal(*agent.Turn.EndedAt) && t.now().Sub(entry.turnEndSeen) < turnEndGrace
}

func (t *tally) recordMiss(agentID uuid.UUID, state agentv1.AgentState) int {
	misses := 0
	t.update(agentID, func(entry *trackedAgent) {
		if entry.missesToward != state {
			entry.missesToward = state
			entry.misses = 0
		}
		entry.misses++
		misses = entry.misses
	})

	return misses
}

func (t *tally) clearMisses(agentID uuid.UUID) {
	t.updateExisting(agentID, func(entry *trackedAgent) {
		entry.misses = 0
		entry.missesToward = agentv1.AgentState_AGENT_STATE_UNSPECIFIED
	})
}

// clearDebounce drops what an agent had accrued towards a transition once a pass decides one,
// leaving the frame hash and the turn end.
func (t *tally) clearDebounce(agentID uuid.UUID) {
	t.updateExisting(agentID, func(entry *trackedAgent) {
		entry.misses = 0
		entry.missesToward = agentv1.AgentState_AGENT_STATE_UNSPECIFIED
		entry.listingsMissing = 0
		entry.shellSince = time.Time{}
		entry.unrecognizedHolds = 0
	})
}

// isAbsenceDoubted reports whether a pane the listing left out is to be waited on rather than
// forgotten, which holds until enough listings in a row have agreed the pane is gone.
func (t *tally) isAbsenceDoubted(agentID uuid.UUID, inListing bool) bool {
	if inListing {
		t.updateExisting(agentID, func(entry *trackedAgent) { entry.listingsMissing = 0 })

		return false
	}

	doubted := false
	t.update(agentID, func(entry *trackedAgent) {
		entry.listingsMissing++
		doubted = entry.listingsMissing < listingsBeforePaneGone
	})

	return doubted
}

// hasShellGraceElapsed reports whether an agent's pane has been showing a bare shell for longer
// than the grace a CLI gets to start, noting the first sighting when this is one.
func (t *tally) hasShellGraceElapsed(agentID uuid.UUID) bool {
	elapsed := false
	t.update(agentID, func(entry *trackedAgent) {
		if entry.shellSince.IsZero() {
			entry.shellSince = t.now()

			return
		}

		elapsed = t.now().Sub(entry.shellSince) >= reconcileShellGrace
	})

	return elapsed
}

func (t *tally) clearShell(agentID uuid.UUID) {
	t.updateExisting(agentID, func(entry *trackedAgent) { entry.shellSince = time.Time{} })
}

// isUnrecognizedHeld records the frame an agent is sitting on and reports whether it differing
// from the one before is still reason to wait rather than demote.
func (t *tally) isUnrecognizedHeld(agentID uuid.UUID, hash uint64) bool {
	held := false
	t.update(agentID, func(entry *trackedAgent) {
		changed := entry.frameHashSeen && hash != entry.frameHash
		entry.frameHash = hash
		entry.frameHashSeen = true

		held = changed && entry.unrecognizedHolds < unrecognizedHoldLimit
		if held {
			entry.unrecognizedHolds++
		}
	})

	return held
}

// markPassed notes that a pass has finished.
func (t *tally) markPassed() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.hasPassed = true
}

// prune forgets the agents the store no longer holds.
func (t *tally) prune(known map[uuid.UUID]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	maps.DeleteFunc(t.tracked, func(id uuid.UUID, _ trackedAgent) bool {
		_, stillKnown := known[id]

		return !stillKnown
	})
}

package agents

import (
	"maps"
	"sync"
	"time"

	agentstore "weave/internal/store/agent"
)

const unrecognizedHoldLimit = 5

const reconcileShellGrace = 10 * time.Second

// listingsBeforePaneGone is how many pane listings in a row have to leave a pane out before it
// is believed gone, since a listing that came back short reads exactly like one that came back
// right and forgetting a live pane costs the agent a binding nothing restores.
const listingsBeforePaneGone = 3

// prRateLimitBackoff is how long a rate-limited agent is left alone before its pull request is
// checked again, kept long because this is one infrequent check per agent rather than a poll
// loop.
const prRateLimitBackoff = 15 * time.Minute

// tally is what the reconcile passes have accrued about each agent, all of it counters and
// timestamps the panes drove rather than anything the store holds.
type tally struct {
	// mu guards tracked and every field of the entries inside it, since an entry is
	// reached by the reconcile pass, by the RPC handler recording a turn boundary, and by
	// the goroutine a pull request check runs on.
	mu      sync.Mutex
	tracked map[string]trackedAgent
	now     func() time.Time
}

// trackedAgent is what one pass leaves the next to work from about a single agent.
type trackedAgent struct {
	misses int
	// missesToward is the state the current run of misses is evidence for, since three passes
	// only settle a change when all three agreed on where the agent was going.
	missesToward      agentstore.State
	listingsMissing   int
	shellSince        time.Time
	turnBoundary      time.Time
	rateLimitedUntil  time.Time
	frameHash         uint64
	frameHashSeen     bool
	unrecognizedHolds int
	reportedReason    string
}

// newTally builds an empty tally over the wall clock.
func newTally() *tally {
	return &tally{tracked: map[string]trackedAgent{}, now: time.Now}
}

// read is what earlier passes have accrued about an agent, the zero value standing for an
// agent nothing has been recorded about yet.
func (t *tally) read(agentID string) trackedAgent {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.tracked[agentID]
}

// update hands fn what has been accrued about an agent and keeps whatever it leaves, starting
// an entry when there is none.
func (t *tally) update(agentID string, fn func(*trackedAgent)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := t.tracked[agentID]
	fn(&entry)
	t.tracked[agentID] = entry
}

// updateExisting is update for the clears, which have nothing to drop for an agent no pass has
// recorded anything about and must not put one in the map by writing.
func (t *tally) updateExisting(agentID string, fn func(*trackedAgent)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.tracked[agentID]
	if !ok {
		return
	}

	fn(&entry)
	t.tracked[agentID] = entry
}

// recordTurnBoundary notes that agentID's own CLI reported its turn ending.
func (t *tally) recordTurnBoundary(agentID string) {
	t.update(agentID, func(entry *trackedAgent) { entry.turnBoundary = t.now() })
}

// recordReportedReason notes what agentID's own CLI reported it was doing, which annotates the
// state the pane decided and never moves it.
func (t *tally) recordReportedReason(agentID, reason string) {
	t.update(agentID, func(entry *trackedAgent) { entry.reportedReason = reason })
}

func (t *tally) readReportedReason(agentID string) string {
	return t.read(agentID).reportedReason
}

func (t *tally) hasFreshTurnBoundary(agentID string, since time.Time) bool {
	return t.read(agentID).turnBoundary.After(since)
}

func (t *tally) recordMiss(agentID string, state agentstore.State) int {
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

func (t *tally) countMisses(agentID string) int {
	return t.read(agentID).misses
}

func (t *tally) clearMisses(agentID string) {
	t.updateExisting(agentID, func(entry *trackedAgent) {
		entry.misses = 0
		entry.missesToward = ""
	})
}

// clearDebounce drops what an agent had accrued towards a transition once one has been
// decided, leaving the frame hash and any rate limit, which both outlive a change of state.
func (t *tally) clearDebounce(agentID string) {
	t.updateExisting(agentID, func(entry *trackedAgent) {
		entry.misses = 0
		entry.missesToward = ""
		entry.listingsMissing = 0
		entry.shellSince = time.Time{}
		entry.turnBoundary = time.Time{}
		entry.unrecognizedHolds = 0
	})
}

// isAbsenceDoubted reports whether a pane the listing left out is to be waited on rather than
// forgotten, which it is until enough listings in a row have agreed the pane is gone.
func (t *tally) isAbsenceDoubted(agentID string, inListing bool) bool {
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
func (t *tally) hasShellGraceElapsed(agentID string) bool {
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

func (t *tally) clearShell(agentID string) {
	t.updateExisting(agentID, func(entry *trackedAgent) { entry.shellSince = time.Time{} })
}

// isUnrecognizedHeld records the frame an agent is sitting on and reports whether its differing
// from the one before it is still reason to wait rather than demote, up to the limit on how long
// a frame that never settles may hold.
func (t *tally) isUnrecognizedHeld(agentID string, hash uint64) bool {
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

func (t *tally) isRateLimited(agentID string) bool {
	return t.now().Before(t.read(agentID).rateLimitedUntil)
}

func (t *tally) backOff(agentID string) {
	t.update(agentID, func(entry *trackedAgent) {
		entry.rateLimitedUntil = t.now().Add(prRateLimitBackoff)
	})
}

// prune forgets the agents the store no longer holds, so an archived or deleted one leaves
// nothing behind to accrue against its id if it ever comes back.
func (t *tally) prune(known map[string]struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	maps.DeleteFunc(t.tracked, func(id string, _ trackedAgent) bool {
		_, stillKnown := known[id]

		return !stillKnown
	})
}

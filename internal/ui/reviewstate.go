package ui

import (
	agentv1 "komrad/proto/gen/agent/v1"
)

// unsentCount is how many comments the reader is holding.
func (t reviewThreads) unsentCount() int {
	return t.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)]
}

// archivedSaid is why a write on an archived agent's review did nothing.
const archivedSaid = "this agent is archived" + separated + "restore it before writing to it"

// isArchived reports whether the agent has left the listing and can take no writes.
func (r reviewScreen) isArchived(m *model) bool {
	_, ok := r.agentRow(m)

	return !ok
}

// holdsUnsent says whether the reader has comments nobody has seen.
func (t reviewThreads) holdsUnsent() bool {
	return t.unsentCount() > 0
}

// isHistory reports whether the round on screen ends before the agent's current work.
func (r reviewScreen) isHistory() (string, bool) {
	rendered := r.diff.spec.head
	if r.diff.View() != nil {
		rendered = r.diff.View().GetSnapshotSHA()
	}

	if rendered == "" || rendered == r.round.GetSnapshotSha() {
		return "", false
	}

	return r.diff.spec.historySaid(r.diff.chapters), true
}

// doesWalk says whether the round keys have anywhere to go, which an editor holding the stage
// takes away.
func (r reviewScreen) doesWalk() bool {
	return !r.hold.atEditor && len(r.diff.chapters) > 0
}

// frozenRound is the round a held review pins the diff to, false while the diff follows the work.
func (r reviewScreen) frozenRound() (*agentv1.ReviewRound, bool) {
	return r.round, r.diff.pinned && r.round != nil
}

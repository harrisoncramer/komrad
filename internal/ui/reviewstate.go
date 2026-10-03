package ui

import (
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	agentv1 "komrad/proto/gen/agent/v1"
)

const reviewWritingTone = paint.ToneSecondary

// statusView names what the reader is part way through, and says nothing when nothing is.
func (r reviewScreen) statusView(m *model) string {
	if r.threads.holdsUnsent() {
		return bandRow(m, reviewWritingTone, "unsent",
			label.Counted(r.threads.unsentCount(), "comment")+" not sent"+separated+
				keyLabel(m.keys.Review.Send)+" sends them to the agent")
	}

	return ""
}

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
	drawn := r.diff.spec.head
	if r.diff.View() != nil {
		drawn = r.diff.View().GetSnapshotSHA()
	}

	if drawn == "" || drawn == r.round.GetSnapshotSha() {
		return "", false
	}

	return r.diff.spec.historySaid(r.diff.chapters), true
}

// doesWalk says whether the round keys have anywhere to go, which an editor holding the stage
// takes away.
func (r reviewScreen) doesWalk() bool {
	return !r.hold.atEditor && len(r.diff.chapters) > 0
}

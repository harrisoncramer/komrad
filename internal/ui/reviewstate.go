package ui

import (
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	agentv1 "komrad/proto/gen/agent/v1"
)

// The editor is the one thing on the screen that changes what the stage holds, so it gets a tone
// apart from the rest of the band.
const (
	reviewWritingTone = paint.ToneSecondary
	reviewHeldTone    = paint.ToneMuted
)

// statusView names what the reader is part way through, and says nothing when nothing is.
func (r reviewScreen) statusView(m *model) string {
	switch {
	case r.hold.atEditor:
		return bandRow(m, reviewWritingTone, "editing",
			r.hold.editingPath+separated+keyLabel(m.keys.Review.EditFile)+" to come back")
	case r.hold.pausing:
		return bandRow(m, reviewWritingTone, "editing", "stopping the agent"+separated+"the editor opens once it has")
	}

	if _, dead := r.isReadOnly(m); dead {
		return bandRow(m, reviewHeldTone, "archived", "this agent is archived, nothing can be sent")
	}

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

// isReadOnly reports whether there is nobody there to receive a comment.
func (r reviewScreen) isReadOnly(m *model) (string, bool) {
	if _, ok := r.agentRow(m); ok {
		return "", false
	}

	return "this agent is archived" + separated + "restore it before writing to it", true
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

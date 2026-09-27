package ui

import (
	"fmt"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	agentv1 "komrad/proto/gen/agent/v1"
)

// The editor is the one thing on the screen that changes what the stage holds, so it gets a tone
// apart from the rest of the band.
const (
	reviewWritingTone = paint.ToneMark
	reviewHeldTone    = paint.ToneMuted
)

// statusView names what the reader is part way through, and says nothing when nothing is.
func (r reviewScreen) statusView(s panelShared) string {
	switch {
	case r.hold.atEditor:
		return bandRow(s, reviewWritingTone, "editing",
			r.hold.editingPath+separated+keyLabel(s.keys.Review.EditFile)+" to come back")
	case r.hold.pausing:
		return bandRow(s, reviewWritingTone, "editing", "stopping the agent"+separated+"the editor opens once it has")
	}

	if _, dead := r.isReadOnly(s); dead {
		return bandRow(s, reviewHeldTone, "gone", "this agent is gone, nothing can be sent")
	}

	if r.threads.holdsUnsent() {
		return bandRow(s, reviewWritingTone, "unsent",
			label.Counted(r.threads.unsentCount(), "comment")+" not sent"+separated+
				keyLabel(formKeys().Submit)+" sends them to the agent")
	}

	return ""
}

// unsentCount is how many comments the reader is holding.
func (t reviewThreads) unsentCount() int {
	return t.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)]
}

// isReadOnly reports whether there is nobody there to receive a comment.
func (r reviewScreen) isReadOnly(s panelShared) (string, bool) {
	for _, item := range s.listing.agents {
		if item.Agent.ID != r.agentID {
			continue
		}

		if item.Alive {
			return "", false
		}

		return "this agent is dormant" + separated + "resume it before writing to it", true
	}

	return "this agent is archived" + separated + "restore it before writing to it", true
}

// holdsUnsent says whether the reader has comments nobody has seen.
func (t reviewThreads) holdsUnsent() bool {
	return t.unsentCount() > 0
}

// isHistory reports whether the round on screen ends before the agent's current work.
func (r reviewScreen) isHistory() (string, bool) {
	drawn := r.diff.head
	if r.diff.view != nil {
		drawn = r.diff.view.GetSnapshotSHA()
	}

	if drawn == "" || drawn == r.round.GetSnapshotSha() {
		return "", false
	}

	at := r.diff.chapter
	if at < 0 || at >= len(r.diff.chapters) {
		return "an earlier chapter", true
	}

	return fmt.Sprintf("chapter %d of %d", at+1, len(r.diff.chapters)), true
}

// doesWalk says whether the round keys have anywhere to go, which an editor holding the stage
// takes away.
func (r reviewScreen) doesWalk() bool {
	return !r.hold.atEditor && len(r.diff.chapters) > 0
}

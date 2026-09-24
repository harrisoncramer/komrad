package ui

import (
	"fmt"

	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	agentv1 "weave/proto/gen/agent/v1"
)

// The mode is the one thing on the screen that changes what every key does, so each state gets
// its own tone.
const (
	reviewWritingTone = paint.ToneMark
	reviewHeldTone    = paint.ToneMuted
)

// reviewMode is which of the two things the review screen is doing.
type reviewMode uint8

const (
	// modeWatching is the mode the screen opens in, where the diff keeps up with the agent and
	// nothing a key does reaches it.
	modeWatching reviewMode = iota
	// modeReviewing is a pass over the work, anchored to a snapshot, with the agent paused and
	// both writing keys available.
	modeReviewing
)

// modeView says which of the two the screen is in, and what to press next.
func (r reviewScreen) modeView(s panelShared) string {
	if r.pass.mode == modeReviewing {
		return bandRow(s, reviewWritingTone, r.pass.label(), r.writingSaid(s))
	}

	return r.watchingView(s)
}

// label is what the band calls the pass, which says the editor is up while it is.
func (p reviewPass) label() string {
	if p.atEditor {
		return "reviewing · editing"
	}

	return "reviewing"
}

// watchingView names whatever stands in the way of a review, and says nothing when nothing does.
func (r reviewScreen) watchingView(s panelShared) string {
	if _, dead := r.isReadOnly(s); dead {
		return bandRow(s, reviewHeldTone, "watching", "this agent is gone, nothing can be sent")
	}

	if r.threads.holdsUnsent() {
		return bandRow(s, reviewHeldTone, "watching",
			label.Counted(r.threads.unsentCount(), "comment")+" not sent · "+
				keyLabel(s.keys.Review.Compose)+" to carry on")
	}

	if r.threads.isOwedAnAnswer() {
		return bandRow(s, reviewHeldTone, "watching",
			reviewOwedSaid(r.owedComments())+" · "+keyLabel(s.keys.Review.NextOpenThread)+
				" walks them, "+keyLabel(s.keys.Review.Resolve)+" settles one, "+
				keyLabel(s.keys.Review.Compose)+" replies")
	}

	return ""
}

// writingSaid is what the pass offers: what is happening to the agent, the file under your
// editor while one is open, and the way out or the way to send.
func (r reviewScreen) writingSaid(s panelShared) string {
	if r.pass.atEditor {
		return r.pass.editingPath + " · " + keyLabel(s.keys.Review.EditFile) + " to come back"
	}

	if r.pass.pausing {
		return "stopping the agent · the pass starts once it has"
	}

	if !r.threads.holdsUnsent() {
		return r.pass.pausedSaid() + keyLabel(s.keys.Review.Back) + " to let it go"
	}

	if r.diff.pinned {
		return r.pass.pausedSaid() + "the code as it was when you started · " +
			keyLabel(formKeys().Submit) + " sends it"
	}

	return r.pass.pausedSaid() + keyLabel(formKeys().Submit) + " sends it to the agent"
}

// pausedSaid opens what the band says with the state of the agent.
func (p reviewPass) pausedSaid() string {
	if !p.paused {
		return ""
	}

	return "agent paused · "
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

		return "this agent is dormant · resume it before writing to it", true
	}

	return "this agent is archived · restore it before writing to it", true
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
		return "an earlier round", true
	}

	return fmt.Sprintf("round %d of %d", at+1, len(r.diff.chapters)), true
}

// doesWalk says whether the round keys have anywhere to go, which watching has and a pass that
// holds the tree does not.
func (r reviewScreen) doesWalk() bool {
	return r.pass.mode == modeWatching && len(r.diff.chapters) > 0
}

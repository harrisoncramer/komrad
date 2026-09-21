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

// reviewModeView says which of the two the screen is in, and what to press next.
func (m model) reviewModeView() string {
	if m.review.mode == modeReviewing {
		return m.reviewBandRow(reviewWritingTone, m.reviewPassLabel(), m.reviewWritingSaid())
	}

	return m.reviewWatchingView()
}

// reviewPassLabel is what the band calls the pass, which says the editor is up while it is.
func (m model) reviewPassLabel() string {
	if m.review.atEditor {
		return "reviewing · editing"
	}

	return "reviewing"
}

// reviewWatchingView names whatever stands in the way of a review, and says nothing when nothing
// does.
func (m model) reviewWatchingView() string {
	if _, dead := m.isReviewReadOnly(); dead {
		return m.reviewBandRow(reviewHeldTone, "watching", "this agent is gone, nothing can be sent")
	}

	if m.doesReviewHoldUnsent() {
		return m.reviewBandRow(reviewHeldTone, "watching",
			label.Counted(m.reviewUnsentCount(), "comment")+" not sent · "+
				keyLabel(m.keys.Review.Compose)+" to carry on")
	}

	if m.review.isOwedAnAnswer() {
		return m.reviewBandRow(reviewHeldTone, "watching",
			reviewOwedSaid(m.reviewOwedComments())+" · "+keyLabel(m.keys.Review.NextOpenThread)+
				" walks them, "+keyLabel(m.keys.Review.Resolve)+" settles one, "+
				keyLabel(m.keys.Review.Compose)+" replies")
	}

	return ""
}

// reviewWritingSaid is what the pass offers: what is happening to the agent, the file under your
// editor while one is open, and the way out or the way to send.
func (m model) reviewWritingSaid() string {
	if m.review.atEditor {
		return m.review.editingPath + " · " + keyLabel(m.keys.Review.EditFile) + " to come back"
	}

	if m.review.pausing {
		return "stopping the agent · the pass starts once it has"
	}

	if !m.doesReviewHoldUnsent() {
		return m.reviewPausedSaid() + keyLabel(m.keys.Review.Back) + " to let it go"
	}

	if m.review.pinned {
		return m.reviewPausedSaid() + "the code as it was when you started · " +
			keyLabel(formKeys().Submit) + " sends it"
	}

	return m.reviewPausedSaid() + keyLabel(formKeys().Submit) + " sends it to the agent"
}

// reviewPausedSaid opens what the band says with the state of the agent.
func (m model) reviewPausedSaid() string {
	if !m.review.paused {
		return ""
	}

	return "agent paused · "
}

// reviewUnsentCount is how many comments the reader is holding.
func (m model) reviewUnsentCount() int {
	return m.review.activity.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)]
}

// isReviewReadOnly reports whether there is nobody there to receive a comment.
func (m model) isReviewReadOnly() (string, bool) {
	for _, item := range m.listing.agents {
		if item.agent.ID != m.review.agentID {
			continue
		}

		if item.alive {
			return "", false
		}

		return "this agent is dormant · resume it before writing to it", true
	}

	return "this agent is archived · restore it before writing to it", true
}

// doesReviewHoldUnsent says whether the reader has comments nobody has seen.
func (m model) doesReviewHoldUnsent() bool {
	return m.reviewUnsentCount() > 0
}

// isReviewHistory reports whether the round on screen ends before the agent's current work.
func (m model) isReviewHistory() (string, bool) {
	drawn := m.review.head
	if m.review.view != nil {
		drawn = m.review.view.GetSnapshotSHA()
	}

	if drawn == "" || drawn == m.review.round.GetSnapshotSha() {
		return "", false
	}

	at := m.review.chapter
	if at < 0 || at >= len(m.review.chapters) {
		return "an earlier round", true
	}

	return fmt.Sprintf("round %d of %d", at+1, len(m.review.chapters)), true
}

// isReviewNarrowed reports whether the diff on screen is one chapter of the change rather than
// the whole of it.
func (m model) isReviewNarrowed() bool {
	return m.review.base != "" || m.review.head != ""
}

// doesReviewWalk says whether the round keys have anywhere to go, which watching has and the two
// modes that hold the tree do not.
func (m model) doesReviewWalk() bool {
	return m.review.mode == modeWatching && len(m.review.chapters) > 0
}

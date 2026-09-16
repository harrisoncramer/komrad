package ui

import (
	"fmt"

	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"
)

// The mode is the one thing on the screen that changes what every key does, so each state gets
// its own tone.
const (
	reviewWatchingTone = paint.ToneAccent
	reviewWritingTone  = paint.ToneMark
	reviewHistoryTone  = paint.ToneWarning
	reviewHeldTone     = paint.ToneMuted
)

// Explainer: editing and commenting were two modes a reader had to choose between before doing
// either, which is not how anybody reads a colleague's work: a line is wrong, and then they either
// fix it or say something about it. Both are the same pass now, and the pass is what pauses the
// agent rather than the edit key.

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

// reviewWatchingView is the mode line while nothing is being written.
func (m model) reviewWatchingView() string {
	if _, dead := m.isReviewReadOnly(); dead {
		return m.reviewBandRow(reviewHeldTone, "watching", "this agent is gone, nothing can be sent")
	}

	if said, history := m.isReviewHistory(); history {
		return m.reviewBandRow(reviewHistoryTone, "reading history",
			said+" is finished · "+keyLabel(m.keys.Review.BaseForward)+" for the current work")
	}

	if m.doesReviewHoldUnsent() {
		return m.reviewBandRow(reviewHeldTone, "watching",
			counted(m.reviewUnsentCount(), "comment")+" not sent · "+
				keyLabel(m.keys.Review.Compose)+" to carry on")
	}

	if m.review.awaiting {
		return m.reviewBandRow(reviewHeldTone, "watching",
			"your review is owed an answer · "+keyLabel(m.keys.Review.NextOpenThread)+
				" for the comments left open, "+keyLabel(m.keys.Review.Compose)+" to reply in one")
	}

	return m.reviewBandRow(reviewWatchingTone, "watching",
		"live · "+keyLabel(m.keys.Review.EditFile)+" to edit · "+
			keyLabel(m.keys.Review.Compose)+" to comment")
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

// reviewPausedSaid opens what the band says with the state of the agent, since stopping it is a
// real thing happening to it that nobody asked for in so many words.
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
	// Explainer: what decides this is the snapshot the diff was drawn to, not what the chapter at
	// the reader's index happens to say now. The list is recut on every load, so an index and a
	// list can disagree, and the refusal has to follow the code on screen rather than the label.
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

// doesReviewWalk says whether the round keys have anywhere to go, which watching has and the two
// modes that hold the tree do not.
func (m model) doesReviewWalk() bool {
	return m.review.mode == modeWatching && len(m.review.chapters) > 0
}

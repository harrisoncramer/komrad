package ui

import (
	"fmt"

	agentv1 "weave/proto/gen/agent/v1"
)

// reviewModeView says whether the reader is looking or writing, since a review is something you
// start rather than something that starts itself, and nothing on the screen said which you were
// doing.
func (m model) reviewModeView() string {
	if _, dead := m.reviewIsReadOnly(); dead {
		return m.reviewBandRow(reviewHeldStyle, "reading", "this agent is gone, nothing can be sent")
	}

	if said, history := m.reviewIsHistory(); history {
		return m.reviewBandRow(reviewHistoryStyle, "reading history",
			said+" is finished · "+keyLabel(m.keys.Review.BaseForward)+" for the current work")
	}

	if m.review.writing {
		return m.reviewBandRow(reviewWritingStyle, "writing a review", m.reviewWritingSaid())
	}

	if m.doesReviewHoldUnsent() {
		return m.reviewBandRow(reviewHeldStyle, "reading",
			counted(m.reviewUnsentCount(), "comment")+" not sent · "+
				keyLabel(m.keys.Review.Compose)+" to carry on")
	}

	if m.review.awaiting {
		return m.reviewBandRow(reviewHeldStyle, "reading", "your review is with the agent")
	}

	return m.reviewBandRow(reviewReadingStyle, "reading",
		"press "+keyLabel(m.keys.Review.Compose)+" to comment")
}

// reviewWritingSaid is what the writing mode offers, which is the way out until something has
// been written and the way to send it once something has.
func (m model) reviewWritingSaid() string {
	if !m.doesReviewHoldUnsent() {
		return "nothing written yet · " + keyLabel(m.keys.Review.Back) + " to go back to reading"
	}

	if m.review.pinned {
		return "the code as it was when you started · " + keyLabel(m.keys.Form.Submit) + " sends it"
	}

	return keyLabel(m.keys.Form.Submit) + " sends it to the agent"
}

// reviewUnsentCount is how many comments the reader is holding.
func (m model) reviewUnsentCount() int {
	return m.review.activity.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)]
}

// reviewIsReadOnly is whether there is nobody there to receive a comment, and what to say about
// it.
func (m model) reviewIsReadOnly() (string, bool) {
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
	return m.review.activity.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)] > 0
}

// reviewIsHistory says whether the round on screen ends before the agent's current work, and
// names it for the refusal to quote.
func (m model) reviewIsHistory() (string, bool) {
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

// doesReviewWalk says whether the round keys have anywhere to go, which reading has and writing
// does not, since a review is written against the whole change.
func (m model) doesReviewWalk() bool {
	return !m.review.writing && len(m.review.chapters) > 0
}

package ui

import (
	"time"

	agentv1 "weave/proto/gen/agent/v1"
)

// findPendingRound is the round a reader's own open review was opened against, and nothing when
// they have none, so a reader part way through a review keeps reading the snapshot they started
// on rather than being moved onto whatever the agent has written since.
func findPendingRound(rounds []*agentv1.ReviewRound, openBatchID string) (*agentv1.ReviewRound, bool) {
	// Explainer: the batch says which snapshot the review is being written against, and a thread
	// does not. Replying into a thread from an earlier round leaves an unsent message on that
	// thread, so reading the round off the thread would drag the whole diff back to whenever that
	// conversation started.
	if openBatchID == "" {
		return nil, false
	}

	for _, round := range rounds {
		if round.GetId() == openBatchID {
			return round, true
		}
	}

	return nil, false
}

// openBatchRoundID is the round the person's unreleased review was opened against, empty when
// they have nothing open.
func openBatchRoundID(reviews []*agentv1.Review) string {
	for _, review := range reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() == "" {
			return review.GetRoundId()
		}
	}

	return ""
}

// findPreviousReviewedRound is the round the person last released a review against, and nothing
// when they have not reviewed this agent before or when its base has moved since.
func findPreviousReviewedRound(
	reviews []*agentv1.Review,
	rounds []*agentv1.ReviewRound,
	round *agentv1.ReviewRound,
) *agentv1.ReviewRound {
	last := findLastHumanReview(reviews, round.GetOrdinal())
	if last == nil {
		return nil
	}

	for _, earlier := range rounds {
		if earlier.GetId() != last.GetRoundId() {
			continue
		}

		// Explainer: two rounds measured from different points are not comparable. History was
		// rewritten under the branch between them, so a diff of the two snapshots would carry
		// whatever else moved on the base as though the agent had written it.
		if earlier.GetBaseSha() != round.GetBaseSha() {
			return nil
		}

		return earlier
	}

	return nil
}

// findLastHumanReview is the newest review the person released against a round earlier than this
// one, which is where they last looked.
func findLastHumanReview(reviews []*agentv1.Review, ordinal int32) *agentv1.Review {
	var last *agentv1.Review
	for _, review := range reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() == "" || review.GetRoundOrdinal() >= ordinal {
			continue
		}

		last = review
	}

	return last
}

// isAwaitingReviewAnswer reports whether the person has released a review against the round on
// screen, the state the service refuses a second review in.
func isAwaitingReviewAnswer(reviews []*agentv1.Review, round *agentv1.ReviewRound) bool {
	for _, review := range reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() != "" && review.GetRoundOrdinal() == round.GetOrdinal() {
			return true
		}
	}

	return false
}

// releasedAtOf is the clock time the person last released a review, which the screen says
// instead of a round number.
func releasedAtOf(reviews []*agentv1.Review, round *agentv1.ReviewRound) string {
	last := findLastHumanReview(reviews, round.GetOrdinal())
	if last == nil {
		return ""
	}

	return releasedClock(last.GetReleasedAt())
}

// releasedClock is when a review was released, in the terms a reader names it by.
func releasedClock(released string) string {
	stamp, err := time.Parse(time.RFC3339, released)
	if err != nil {
		return ""
	}

	return stamp.Local().Format("3:04PM")
}

package assemble

import (
	"cmp"
	"slices"
	"strings"

	agentv1 "weave/proto/gen/agent/v1"

	"google.golang.org/protobuf/proto"
)

// ReviewThreads is the conversation on an agent's diff, with each comment's delivery read off its review.
func ReviewThreads(w World, agentID string) []*agentv1.ReviewThread {
	var threads []*agentv1.ReviewThread
	for _, held := range w.ReviewThreads {
		if held.GetAgentId() != agentID {
			continue
		}

		thread, ok := proto.Clone(held).(*agentv1.ReviewThread)
		if !ok {
			continue
		}
		for _, message := range thread.GetMessages() {
			setDelivery(message, w.Reviews)
		}

		threads = append(threads, thread)
	}

	slices.SortFunc(threads, compareThreads)

	return threads
}

// setDelivery reads a comment's delivery off the review it went out with.
func setDelivery(message *agentv1.ReviewMessage, reviews map[string]*agentv1.WatchedReview) {
	if message.ReviewId == nil {
		return
	}

	review := reviews[message.GetReviewId()]
	message.SentAt = review.GetReleasedAt()
	message.DeliveredAt = review.GetDeliveredAt()

	switch {
	case message.GetSentAt() == "":
		message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT
	case message.GetDeliveredAt() != "":
		message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED
	default:
		message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT
	}
}

// compareThreads orders threads by round, then by where they sit in the diff.
func compareThreads(a, b *agentv1.ReviewThread) int {
	return cmp.Or(
		cmp.Compare(a.GetRoundOrdinal(), b.GetRoundOrdinal()),
		strings.Compare(a.GetFilePath(), b.GetFilePath()),
		cmp.Compare(a.GetNewStart(), b.GetNewStart()),
		cmp.Compare(a.GetOldStart(), b.GetOldStart()),
		strings.Compare(a.GetId(), b.GetId()),
	)
}

package ui

import (
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewDeliveryBadgeNamesTheMoment pins the time on the badge being the last thing that
// happened to the comment.
func TestReviewDeliveryBadgeNamesTheMoment(t *testing.T) {
	sent := sentComment()
	delivered := deliveredComment()

	assert.Equal(t, sent.GetSentAt(), reviewDeliveryStamp(sent))
	assert.Equal(t, delivered.GetDeliveredAt(), reviewDeliveryStamp(delivered),
		"once it has landed, when it went is no longer the news")
	assert.Empty(t, reviewDeliveryStamp(unsentComment()), "nothing has happened to it yet")
}

func unsentComment() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         "m1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
	}
}

func sentComment() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         "m1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
		SentAt:     "2026-09-02T10:00:00Z",
	}
}

func deliveredComment() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:          "m2",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Delivery:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
		SentAt:      "2026-09-02T10:00:00Z",
		DeliveredAt: "2026-09-02T10:01:00Z",
	}
}

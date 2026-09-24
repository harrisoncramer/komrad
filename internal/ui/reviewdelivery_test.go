package ui

import (
	"testing"

	agentv1 "komrade/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewDeliveryStamp covers the time on a badge being the last thing that happened to the comment.
func TestReviewDeliveryStamp(t *testing.T) {
	tests := []struct {
		name    string
		message *agentv1.ReviewMessage
		want    string
	}{
		{
			name:    "an unsent comment has no moment yet",
			message: &agentv1.ReviewMessage{Delivery: agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT},
			want:    "",
		},
		{
			name: "a sent comment names when it went",
			message: &agentv1.ReviewMessage{
				Delivery: agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
				SentAt:   "2026-09-02T10:00:00Z",
			},
			want: "2026-09-02T10:00:00Z",
		},
		{
			name: "a delivered comment names when it landed",
			message: &agentv1.ReviewMessage{
				Delivery:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
				SentAt:      "2026-09-02T10:00:00Z",
				DeliveredAt: "2026-09-02T10:01:00Z",
			},
			want: "2026-09-02T10:01:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, reviewDeliveryStamp(tt.message))
		})
	}
}

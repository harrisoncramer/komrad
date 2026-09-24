package assemble_test

import (
	"testing"

	"weave/internal/assemble"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewThreadsWorksOutDelivery pins each comment's delivery read off its review in the world,
// and an answer from the agent carrying none.
func TestReviewThreadsWorksOutDelivery(t *testing.T) {
	tests := []struct {
		name        string
		review      *agentv1.WatchedReview
		reviewID    *string
		wants       agentv1.ReviewDeliveryState
		sentAt      string
		deliveredAt string
	}{
		{
			name:     "an answer from the agent",
			reviewID: nil,
			wants:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED,
		},
		{
			name:     "a comment in a review still open",
			review:   &agentv1.WatchedReview{Id: "r1"},
			reviewID: xptr.Ptr("r1"),
			wants:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
		},
		{
			name:     "a comment in a released review",
			review:   &agentv1.WatchedReview{Id: "r1", ReleasedAt: "2026-09-02T10:00:00Z"},
			reviewID: xptr.Ptr("r1"),
			wants:    agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
			sentAt:   "2026-09-02T10:00:00Z",
		},
		{
			name:        "a comment whose notice the agent claimed",
			review:      &agentv1.WatchedReview{Id: "r1", ReleasedAt: "2026-09-02T10:00:00Z", DeliveredAt: "2026-09-02T10:05:00Z"},
			reviewID:    xptr.Ptr("r1"),
			wants:       agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
			sentAt:      "2026-09-02T10:00:00Z",
			deliveredAt: "2026-09-02T10:05:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w assemble.World
			if tt.review != nil {
				w.Put(&watchv1.Row{Row: &watchv1.Row_Review{Review: tt.review}})
			}
			held := threadOf("t1", "a1", 1, "main.go", 1)
			held.Messages = []*agentv1.ReviewMessage{{Id: "m1", ReviewId: tt.reviewID}}
			w.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: held}})

			threads := assemble.ReviewThreads(w, "a1")
			require.Len(t, threads, 1)
			message := threads[0].GetMessages()[0]

			assert.Equal(t, tt.wants, message.GetDelivery())
			assert.Equal(t, tt.sentAt, message.GetSentAt())
			assert.Equal(t, tt.deliveredAt, message.GetDeliveredAt())
			assert.Equal(t, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED, held.GetMessages()[0].GetDelivery(),
				"the world's own row is left as the watch sent it")
		})
	}
}

// TestReviewThreadsRunsDownTheDiff pins the order: one agent's threads, by round, then file, then
// line.
func TestReviewThreadsRunsDownTheDiff(t *testing.T) {
	var w assemble.World
	for _, thread := range []*agentv1.ReviewThread{
		threadOf("t4", "a1", 2, "a.go", 1),
		threadOf("t3", "a1", 1, "b.go", 1),
		threadOf("t2", "a1", 1, "a.go", 9),
		threadOf("t1", "a1", 1, "a.go", 3),
		threadOf("t5", "a2", 1, "a.go", 1),
	} {
		w.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: thread}})
	}

	ids := xslice.Map(assemble.ReviewThreads(w, "a1"), func(thread *agentv1.ReviewThread) string { return thread.GetId() })

	assert.Equal(t, []string{"t1", "t2", "t3", "t4"}, ids)
}

func threadOf(id, agentID string, round int32, file string, line int32) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:           id,
		AgentId:      agentID,
		RoundOrdinal: round,
		FilePath:     file,
		NewStart:     line,
	}
}

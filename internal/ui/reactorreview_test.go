package ui

import (
	"testing"

	"weave/internal/client"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewReactorFollowsABatch pins that a batch from the watch reads the review's
// conversation again once the review has moved, and only while the review is up.
func TestReviewReactorFollowsABatch(t *testing.T) {
	tests := []struct {
		name   string
		review bool
		reads  bool
	}{
		{
			name:   "another screen reads nothing",
			review: false,
			reads:  false,
		},
		{
			name:   "the review screen reads the move",
			review: true,
			reads:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m.reactors = newWorldReactors()
			if tt.review {
				m = openReview(t, m)
			}

			m, _ = update(t, m, client.WatchBatch{putEvent(agentRowOf(&agentv1.Agent{Id: "a1", ReviewVersion: "1"}))})

			assert.Equal(t, tt.reads, m.review.threads.reading)
		})
	}
}

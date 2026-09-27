package ui

import (
	"testing"

	"komrad/internal/assemble"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewWorldReactorFollowsWhatTheReviewDraws pins which batches the review reads the world
// again for.
func TestReviewWorldReactorFollowsWhatTheReviewDraws(t *testing.T) {
	tests := []struct {
		name    string
		changed assemble.Changed
		moves   bool
	}{
		{
			name:    "a snapshot",
			changed: assemble.Changed{Replaced: true},
			moves:   true,
		},
		{
			name:    "a thread",
			changed: changedKeys(watchv1.Resource_RESOURCE_REVIEW_THREAD, "t1"),
			moves:   true,
		},
		{
			name:    "a review",
			changed: changedKeys(watchv1.Resource_RESOURCE_REVIEW, "r1"),
			moves:   true,
		},
		{
			name:    "the agent under review",
			changed: changedKeys(watchv1.Resource_RESOURCE_AGENT, "a1"),
			moves:   true,
		},
		{
			name:    "another agent",
			changed: changedKeys(watchv1.Resource_RESOURCE_AGENT, "a2"),
			moves:   false,
		},
		{
			name:    "mail",
			changed: changedKeys(watchv1.Resource_RESOURCE_MAIL, "n1"),
			moves:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.moves, doesMoveReview(tt.changed, "a1"))
		})
	}
}

func changedKeys(resource watchv1.Resource, key string) assemble.Changed {
	return assemble.Changed{Keys: map[watchv1.Resource][]string{resource: {key}}}
}

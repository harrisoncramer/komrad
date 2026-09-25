package ui

import (
	"testing"

	"komrad/internal/assemble"
	agentv1 "komrad/proto/gen/agent/v1"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
)

// TestReviewOwedIsReadFromTheWorld pins that the review still owed an answer is the person's last
// released one while a thread it opened is unsettled.
func TestReviewOwedIsReadFromTheWorld(t *testing.T) {
	tests := []struct {
		name     string
		resolved string
		wants    string
	}{
		{
			name:  "an unsettled thread is owed an answer",
			wants: "r2",
		},
		{
			name:     "a settled one is not",
			resolved: "2026-09-02T11:00:00Z",
			wants:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w assemble.World
			w.Put(reviewRowOf("r1", "2026-09-02T09:00:00Z", ""))
			w.Put(reviewRowOf("r2", "2026-09-02T10:00:00Z", ""))
			w.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: &agentv1.ReviewThread{
				Id:         "t1",
				AgentId:    "a1",
				ReviewId:   "r2",
				ResolvedAt: tt.resolved,
			}}})

			assert.Equal(t, tt.wants, findOwedReview(w, "a1"))
		})
	}
}

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

// reviewRowOf is a review of a1's work the person released, delivered when deliveredAt is set.
func reviewRowOf(id, releasedAt, deliveredAt string) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Review{Review: &agentv1.WatchedReview{
		Id:          id,
		AgentId:     "a1",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:  releasedAt,
		DeliveredAt: deliveredAt,
	}}}
}

func changedKeys(resource watchv1.Resource, key string) assemble.Changed {
	return assemble.Changed{Keys: map[watchv1.Resource][]string{resource: {key}}}
}

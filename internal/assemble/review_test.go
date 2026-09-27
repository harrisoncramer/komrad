package assemble_test

import (
	"strconv"
	"testing"
	"time"

	"komrad/internal/assemble"

	agentv1 "komrad/proto/gen/agent/v1"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestAgentRowsLocalReview pins the rounds a row counts: released rounds, each once, against the
// agent whose work went round.
func TestAgentRowsLocalReview(t *testing.T) {
	t.Parallel()

	commented := agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED

	tests := []struct {
		name    string
		reviews []*agentv1.WatchedReview
		rounds  int32
	}{
		{
			name: "no review is nothing",
		},
		{
			name:    "an open review is counted against nobody",
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, "", commented)},
		},
		{
			name:    "a review of another agent's work is not this agent's",
			reviews: []*agentv1.WatchedReview{review("v1", older, 1, stamp(-time.Hour), commented)},
		},
		{
			name: "two reviews of one round count it once",
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-time.Hour), commented),
				review("v2", newer, 1, stamp(-2*time.Hour), commented),
			},
			rounds: 1,
		},
		{
			name: "every released round counts",
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-3*time.Hour), commented),
				review("v2", newer, 2, stamp(-2*time.Hour), commented),
				review("v3", newer, 3, stamp(-time.Hour), commented),
			},
			rounds: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			world := assemble.World{
				Agents:  map[string]*agentv1.Agent{newer: agent(newer)},
				Reviews: map[string]*agentv1.WatchedReview{},
			}
			for _, review := range tt.reviews {
				world.Reviews[review.GetId()] = review
			}

			rows := assemble.AgentRows(world, now)
			require.Len(t, rows, 1)
			assert.Equal(t, tt.rounds, rows[0].GetLocalReviewRounds())
		})
	}
}

// TestAgentRowsCommentCounts pins how the comments on an agent's work are counted: reviewer
// agents apart from the person, open conversations on any round, resolved ones only until their
// round lands, and nothing from a batch still being written.
func TestAgentRowsCommentCounts(t *testing.T) {
	t.Parallel()

	commented := agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED
	landed := stamp(-time.Minute)

	tests := []struct {
		name         string
		reviews      []*agentv1.WatchedReview
		threads      []*agentv1.ReviewThread
		wantReviewer *agentv1.CommentCounts
		wantPerson   *agentv1.CommentCounts
	}{
		{
			name:    "a batch still being written draws nothing",
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, "", commented)},
			threads: []*agentv1.ReviewThread{thread("t1", "v1", false, true)},
		},
		{
			name:    "a released review of a landed round with every conversation resolved draws nothing",
			reviews: []*agentv1.WatchedReview{landedOn(review("v1", newer, 1, stamp(-time.Hour), commented), landed)},
			threads: []*agentv1.ReviewThread{thread("t1", "v1", true, false)},
		},
		{
			name:    "a released review with no comments draws nothing",
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, stamp(-time.Hour), commented)},
		},
		{
			name: "an open conversation on a landed round still counts",
			reviews: []*agentv1.WatchedReview{
				landedOn(review("v1", newer, 1, stamp(-time.Hour), commented), landed),
			},
			threads: []*agentv1.ReviewThread{
				thread("t1", "v1", false, false),
				thread("t2", "v1", true, true),
			},
			wantPerson: &agentv1.CommentCounts{Open: 1},
		},
		{
			name: "a reviewer agent that left no comments draws nothing beside the person's",
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-2*time.Hour), commented),
				byReviewer(review("v2", newer, 1, stamp(-time.Hour), commented)),
			},
			threads:    []*agentv1.ReviewThread{thread("t1", "v1", false, false)},
			wantPerson: &agentv1.CommentCounts{Open: 1},
		},
		{
			name: "reviewer agents and the person are counted apart",
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-2*time.Hour), commented),
				byReviewer(review("v2", newer, 1, stamp(-time.Hour), commented)),
			},
			threads: []*agentv1.ReviewThread{
				thread("t1", "v1", false, false),
				thread("t2", "v1", true, true),
				thread("t3", "v2", false, false),
				thread("t4", "v2", false, false),
				thread("t5", "v2", true, false),
			},
			wantReviewer: &agentv1.CommentCounts{
				Open:     2,
				Resolved: 1,
			},
			wantPerson: &agentv1.CommentCounts{
				Open:     1,
				Resolved: 1,
				Unread:   true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			world := assemble.World{
				Agents:        map[string]*agentv1.Agent{newer: agent(newer)},
				Reviews:       map[string]*agentv1.WatchedReview{},
				ReviewThreads: map[string]*agentv1.ReviewThread{},
			}
			for _, review := range tt.reviews {
				world.Reviews[review.GetId()] = review
			}
			for _, thread := range tt.threads {
				world.ReviewThreads[thread.GetId()] = thread
			}

			rows := assemble.AgentRows(world, now)
			require.Len(t, rows, 1)
			assert.True(t, proto.Equal(tt.wantReviewer, rows[0].GetReviewerComments()), "reviewer: %v", rows[0].GetReviewerComments())
			assert.True(t, proto.Equal(tt.wantPerson, rows[0].GetPersonComments()), "person: %v", rows[0].GetPersonComments())
		})
	}
}

// TestApplyReview pins that a review put and dropped by the watch is what the rows count.
func TestApplyReview(t *testing.T) {
	t.Parallel()

	var world assemble.World
	world.Apply(put(agentRow(agent(newer))))
	changed := world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Review{Review: review("v1", newer, 1, stamp(-time.Hour), agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED)}}))
	assert.Equal(t, map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_REVIEW: {"v1"}}, changed.Keys)
	assert.Equal(t, int32(1), assemble.AgentRows(world, now)[0].GetLocalReviewRounds())

	world.Apply(deleted(watchv1.Resource_RESOURCE_REVIEW, "v1"))
	assert.Empty(t, world.Reviews)
	assert.Zero(t, assemble.AgentRows(world, now)[0].GetLocalReviewRounds())
}

func review(id, agentID string, ordinal int32, releasedAt string, verdict agentv1.ReviewVerdict) *agentv1.WatchedReview {
	watched := &agentv1.WatchedReview{
		Id:               id,
		AgentId:          agentID,
		RoundId:          agentID + "-round-" + strconv.Itoa(int(ordinal)),
		RoundOrdinal:     ordinal,
		RoundSnapshotSha: "snapshot-" + strconv.Itoa(int(ordinal)),
		ReleasedAt:       releasedAt,
	}
	if releasedAt != "" {
		watched.Verdict = verdict
	}

	return watched
}

func landedOn(review *agentv1.WatchedReview, at string) *agentv1.WatchedReview {
	review.RoundLandedAt = at

	return review
}

func byReviewer(review *agentv1.WatchedReview) *agentv1.WatchedReview {
	review.AuthorKind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT

	return review
}

func thread(id, reviewID string, resolved, unread bool) *agentv1.ReviewThread {
	thread := &agentv1.ReviewThread{
		Id:            id,
		AgentId:       newer,
		ReviewId:      reviewID,
		UnreadByHuman: unread,
	}
	if resolved {
		thread.ResolvedAt = stamp(-time.Minute)
	}

	return thread
}

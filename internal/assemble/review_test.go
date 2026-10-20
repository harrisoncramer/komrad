package assemble_test

import (
	"strconv"
	"testing"
	"time"

	"komrad/internal/assemble"
	"komrad/lib/xptr"

	agentv1 "komrad/proto/gen/agent/v1"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestTaskRowsLocalReview pins the snapshots a row counts: released snapshots, each once, against the
// task whose work went round.
func TestTaskRowsLocalReview(t *testing.T) {
	t.Parallel()

	commented := agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED

	tests := []struct {
		name      string
		reviews   []*agentv1.WatchedReview
		snapshots int32
	}{
		{
			name: "no review is nothing",
		},
		{
			name:    "an open review is counted against nobody",
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, "", commented)},
		},
		{
			name:    "a review of another task's work is not this task's",
			reviews: []*agentv1.WatchedReview{review("v1", older, 1, stamp(-time.Hour), commented)},
		},
		{
			name: "two reviews of one snapshot count it once",
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-time.Hour), commented),
				review("v2", newer, 1, stamp(-2*time.Hour), commented),
			},
			snapshots: 1,
		},
		{
			name: "every released snapshot counts",
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-3*time.Hour), commented),
				review("v2", newer, 2, stamp(-2*time.Hour), commented),
				review("v3", newer, 3, stamp(-time.Hour), commented),
			},
			snapshots: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			world := assemble.World{
				Tasks:   tasks(task(newer)),
				Reviews: map[string]*agentv1.WatchedReview{},
			}
			for _, review := range tt.reviews {
				world.Reviews[review.GetId()] = review
			}

			assert.Equal(t, tt.snapshots, assemble.TaskRows(world)["task-"+newer].LocalReviewSnapshots)
		})
	}
}

// TestTaskRowsCommentCounts pins how the comments on a task's work are counted: reviewer
// agents apart from the person, open conversations on any snapshot, resolved ones only until their
// snapshot lands, and nothing from a batch still being written.
func TestTaskRowsCommentCounts(t *testing.T) {
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
			name:    "a released review of a landed snapshot with every conversation resolved draws nothing",
			reviews: []*agentv1.WatchedReview{landedOn(review("v1", newer, 1, stamp(-time.Hour), commented), landed)},
			threads: []*agentv1.ReviewThread{thread("t1", "v1", true, false)},
		},
		{
			name:    "a released review with no comments draws nothing",
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, stamp(-time.Hour), commented)},
		},
		{
			name: "an open conversation on a landed snapshot still counts",
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
				thread("t1", "v1", false, true),
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
				Unread:   2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			world := assemble.World{
				Tasks:         tasks(task(newer)),
				Reviews:       map[string]*agentv1.WatchedReview{},
				ReviewThreads: map[string]*agentv1.ReviewThread{},
			}
			for _, review := range tt.reviews {
				world.Reviews[review.GetId()] = review
			}
			for _, thread := range tt.threads {
				world.ReviewThreads[thread.GetId()] = thread
			}

			row := assemble.TaskRows(world)["task-"+newer]
			assert.True(t, proto.Equal(tt.wantReviewer, row.ReviewerComments), "reviewer: %v", row.ReviewerComments)
			assert.True(t, proto.Equal(tt.wantPerson, row.PersonComments), "person: %v", row.PersonComments)
		})
	}
}

// TestAgentRowsLocalReviewOnTheWriter pins that a task's local review is drawn beside its writer,
// and not beside a reviewer on the same task.
func TestAgentRowsLocalReviewOnTheWriter(t *testing.T) {
	t.Parallel()

	reviewer := agent(older)
	reviewer.TaskId = xptr.Ptr("task-" + newer)
	reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
	world := assemble.World{
		Agents:  map[string]*agentv1.Agent{newer: agent(newer), older: reviewer},
		Tasks:   tasks(task(newer)),
		Reviews: map[string]*agentv1.WatchedReview{},
	}
	released := review("v1", newer, 1, stamp(-time.Hour), agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED)
	world.Reviews[released.GetId()] = released

	rows := assemble.AgentRows(world, now)
	require.Len(t, rows, 2)
	assert.Equal(t, newer, rows[0].GetAgent().GetId())
	assert.Equal(t, int32(1), rows[0].GetLocalReviewSnapshots())
	assert.Zero(t, rows[1].GetLocalReviewSnapshots())
}

// TestApplyReview pins that a review put and dropped by the watch is what the rows count.
func TestApplyReview(t *testing.T) {
	t.Parallel()

	var world assemble.World
	world.Apply(put(agentRow(agent(newer))))
	changed := world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Review{Review: review("v1", newer, 1, stamp(-time.Hour), agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED)}}))
	assert.Equal(t, map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_REVIEW: {"v1"}}, changed.Keys)
	assert.Equal(t, int32(1), assemble.AgentRows(world, now)[0].GetLocalReviewSnapshots())

	world.Apply(deleted(watchv1.Resource_RESOURCE_REVIEW, "v1"))
	assert.Empty(t, world.Reviews)
	assert.Zero(t, assemble.AgentRows(world, now)[0].GetLocalReviewSnapshots())
}

func review(id, agentID string, ordinal int32, releasedAt string, verdict agentv1.ReviewVerdict) *agentv1.WatchedReview {
	watched := &agentv1.WatchedReview{
		Id:              id,
		TaskId:          "task-" + agentID,
		SnapshotId:      agentID + "-snapshot-" + strconv.Itoa(int(ordinal)),
		SnapshotOrdinal: ordinal,
		SnapshotSha:     "snapshot-" + strconv.Itoa(int(ordinal)),
		ReleasedAt:      releasedAt,
	}
	if releasedAt != "" {
		watched.Verdict = verdict
	}

	return watched
}

func landedOn(review *agentv1.WatchedReview, at string) *agentv1.WatchedReview {
	review.SnapshotLandedAt = at

	return review
}

func byReviewer(review *agentv1.WatchedReview) *agentv1.WatchedReview {
	review.AuthorKind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT

	return review
}

func thread(id, reviewID string, resolved, unread bool) *agentv1.ReviewThread {
	thread := &agentv1.ReviewThread{
		Id:            id,
		TaskId:        "task-" + newer,
		ReviewId:      reviewID,
		UnreadByHuman: unread,
	}
	if resolved {
		thread.ResolvedAt = stamp(-time.Minute)
	}

	return thread
}

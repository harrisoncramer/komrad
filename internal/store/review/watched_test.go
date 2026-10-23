package review

import (
	"testing"

	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatched pins that every review is carried, released or not, with its snapshot, and
// that keys and tasks narrow it.
func TestListWatched(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	worker := st.seedAgent(t, "worker")
	reviewer := st.seedReviewer(t, worker)
	other := st.seedAgent(t, "other")

	released := st.openReviewBy(t, st.takeSnapshot(t, worker, "one"), reviewer)
	st.releaseReview(t, released, agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED)
	pending := st.openReviewBy(t, st.takeSnapshot(t, worker, "two"), reviewer)
	elsewhere := st.openReviewBy(t, st.takeSnapshot(t, other, "three"), st.seedReviewer(t, other))

	snapshot, err := st.ListWatched(ctx, nil)
	require.NoError(t, err)
	require.Len(t, snapshot, 3)
	byID := xslice.KeyBy(snapshot, func(review Review) uuid.UUID { return review.ID })

	assert.Equal(t, int32(1), byID[released.ID].SnapshotOrdinal)
	assert.Equal(t, "snapshot-one", byID[released.ID].SnapshotSHA)
	assert.NotNil(t, byID[released.ID].ReleasedAt)
	assert.Equal(t, int32(2), byID[pending.ID].SnapshotOrdinal)
	assert.Nil(t, byID[pending.ID].ReleasedAt, "an open review is carried too")

	listed, err := st.ListWatched(ctx, []uuid.UUID{released.ID})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, released.ID, listed[0].ID)

	ofOther, err := st.ListIDsOfTasks(ctx, []uuid.UUID{st.taskOf(t, other)})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{elsewhere.ID}, ofOther)

	ofNobody, err := st.ListIDsOfTasks(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, ofNobody, "no tasks is no reviews rather than every review")
}

// TestListWatchedThreads pins that the threads of live agents are carried with their messages
// and snapshot, and that an archived agent's threads leave the set.
func TestListWatchedThreads(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	threads := NewThreadStore(st.pool)
	worker := st.seedAgent(t, "worker")
	archived := st.seedAgent(t, "archived")

	snapshotOne := st.takeSnapshot(t, worker, "one")
	thread := st.comment(t, snapshotOne, "wrong greeting")
	st.writeAnswer(t, thread)
	reviewer := st.seedReviewer(t, worker)
	opened := st.openThread(t, st.openReviewBy(t, snapshotOne, reviewer), "name this")
	gone := st.comment(t, st.takeSnapshot(t, archived, "two"), "left behind")

	_, err := st.agents.Archive(ctx, []uuid.UUID{archived})
	require.NoError(t, err)

	snapshot, err := threads.ListWatched(ctx, nil)
	require.NoError(t, err)
	require.Len(t, snapshot, 2, "the archived agent's thread is not watched")
	byID := xslice.KeyBy(snapshot, func(thread ThreadWithMessages) uuid.UUID { return thread.ID })
	assert.Equal(t, agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN, byID[thread.ID].AuthorKind)
	assert.Nil(t, byID[thread.ID].AuthorAgentID)
	assert.Equal(t, int32(1), byID[thread.ID].SnapshotOrdinal)
	assert.True(t, byID[thread.ID].UnreadByHuman, "the agent answered since the person looked")
	require.Len(t, byID[thread.ID].Messages, 2)
	assert.Equal(t, "wrong greeting", byID[thread.ID].Messages[0].Body)
	assert.Equal(t, agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT, byID[opened.ID].AuthorKind)
	assert.Equal(t, &reviewer, byID[opened.ID].AuthorAgentID)

	listed, err := threads.ListWatched(ctx, []uuid.UUID{gone.ID})
	require.NoError(t, err)
	assert.Empty(t, listed, "a key on an archived agent is gone")

	ofWorker, err := threads.ListIDsOfTasks(ctx, []uuid.UUID{st.taskOf(t, worker)})
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{thread.ID, opened.ID}, ofWorker)

	ofNobody, err := threads.ListIDsOfTasks(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, ofNobody, "no tasks is no threads rather than every thread")
}

// TestListWatchedThreadsCarriesForgeReplies pins that a mirrored thread carries the forge thread it
// mirrors and its replies, and that a thread opened in komrad carries neither.
func TestListWatchedThreadsCarriesForgeReplies(t *testing.T) {
	st := openFixture(t)
	seeded := st.seedReplyThreads(t)
	reply := st.writeIn(t, seeded, agentv1.ForgeReplyState_FORGE_REPLY_STATE_APPROVED)

	snapshot, err := NewThreadStore(st.pool).ListWatched(t.Context(), nil)
	require.NoError(t, err)
	byID := xslice.KeyBy(snapshot, func(thread ThreadWithMessages) uuid.UUID { return thread.ID })

	mirrored := byID[seeded.mirrored]
	assert.NotNil(t, mirrored.ForgeThreadID)
	require.Len(t, mirrored.ForgeReplies, 1)
	assert.Equal(t, reply.ID, mirrored.ForgeReplies[0].ID)

	local := byID[seeded.local]
	assert.Nil(t, local.ForgeThreadID)
	assert.Empty(t, local.ForgeReplies)
}

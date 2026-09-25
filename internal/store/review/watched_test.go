package review

import (
	"testing"

	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatched pins that every review is carried, released or not, with its round and when
// its doorbell was claimed, and that keys and agents narrow it.
func TestListWatched(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	worker := st.seedAgent(t, "worker")
	other := st.seedAgent(t, "other")

	released := st.openReview(t, st.openRound(t, worker, "one"))
	st.releaseReview(t, released, VerdictApproved)
	pending := st.openReview(t, st.openRound(t, worker, "two"))
	elsewhere := st.openReview(t, st.openRound(t, other, "three"))

	snapshot, err := st.ListWatched(ctx, nil)
	require.NoError(t, err)
	require.Len(t, snapshot, 3)
	byID := xslice.KeyBy(snapshot, func(review Review) string { return review.ID })

	assert.Equal(t, int32(1), byID[released.ID].RoundOrdinal)
	assert.Equal(t, "snapshot-one", byID[released.ID].RoundSnapshotSHA)
	assert.NotNil(t, byID[released.ID].ReleasedAt)
	assert.Nil(t, byID[released.ID].DeliveredAt, "the doorbell is still waiting")
	assert.Equal(t, int32(2), byID[pending.ID].RoundOrdinal)
	assert.Nil(t, byID[pending.ID].ReleasedAt, "an open review is carried too")

	_, err = st.mail.Claim(ctx, worker, 10)
	require.NoError(t, err)

	listed, err := st.ListWatched(ctx, []string{released.ID})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].DeliveredAt, "claiming the doorbell delivers the review")

	ofOther, err := st.ListIDsOfAgents(ctx, []string{other, "not-an-id"})
	require.NoError(t, err)
	assert.Equal(t, []string{elsewhere.ID}, ofOther)

	ofNobody, err := st.ListIDsOfAgents(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, ofNobody, "no agents is no reviews rather than every review")
}

// TestListWatchedThreads pins that the threads of live agents are carried with their messages
// and round, without any message's delivery, and that an archived agent's threads leave the set.
func TestListWatchedThreads(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	threads := NewThreadStore(st.pool)
	worker := st.seedAgent(t, "worker")
	archived := st.seedAgent(t, "archived")

	thread := st.openThread(t, st.openReview(t, st.openRound(t, worker, "one")), "wrong greeting")
	st.writeAnswer(t, thread)
	gone := st.openThread(t, st.openReview(t, st.openRound(t, archived, "two")), "left behind")

	_, err := st.agents.Archive(ctx, []string{archived})
	require.NoError(t, err)

	snapshot, err := threads.ListWatched(ctx, nil)
	require.NoError(t, err)
	require.Len(t, snapshot, 1, "the archived agent's thread is not watched")
	assert.Equal(t, thread.ID, snapshot[0].ID)
	assert.Equal(t, int32(1), snapshot[0].RoundOrdinal)
	assert.True(t, snapshot[0].UnreadByHuman, "the agent answered since the person looked")
	require.Len(t, snapshot[0].Messages, 2)
	assert.Equal(t, "wrong greeting", snapshot[0].Messages[0].Body)
	assert.Equal(t, DeliveryNone, snapshot[0].Messages[0].Delivery, "delivery is the panel's to work out")

	listed, err := threads.ListWatched(ctx, []string{gone.ID, "not-a-uuid"})
	require.NoError(t, err)
	assert.Empty(t, listed, "a key on an archived agent is gone")
}

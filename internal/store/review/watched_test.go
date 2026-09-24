package review

import (
	"testing"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatched pins that every review is carried, released or not, with its round and when
// its doorbell was claimed, and that keys and agents narrow it.
func TestListWatched(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	worker := seedAgent(t, st)
	other := seedAgentNamed(t, st, "other")

	first := openRound(t, st, worker, "one")
	second := openRound(t, st, worker, "two")
	released := openReview(t, st, worker, first.ID)
	release(t, st, worker, released.ID, VerdictApproved)
	pending := openReview(t, st, worker, second.ID)
	elsewhere := openReview(t, st, other, openRound(t, st, other, "three").ID)

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

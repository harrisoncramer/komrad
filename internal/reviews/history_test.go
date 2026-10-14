package reviews

import (
	"testing"

	"komrad/internal/fail"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetHistoryPicksTheRound covers which round the panel opens a review on.
func TestGetHistoryPicksTheRound(t *testing.T) {
	tests := []struct {
		name        string
		arrange     func(t *testing.T, fx *reviewFixture)
		fresh       bool
		wantOrdinal int32
		wantRounds  int
		wantPinned  bool
		wantNotice  string
	}{
		{
			name:        "the tree as it stands is taken as a new round",
			arrange:     func(*testing.T, *reviewFixture) {},
			wantOrdinal: 2,
			wantRounds:  2,
		},
		{
			name: "an open review holds the round it was written on",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
			},
			wantOrdinal: 1,
			wantRounds:  1,
			wantPinned:  true,
		},
		{
			name: "a fresh read lets go of the open review's round",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
			},
			fresh:       true,
			wantOrdinal: 2,
			wantRounds:  2,
		},
		{
			name: "an agent's open review pins nothing for the person",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
			},
			wantOrdinal: 2,
			wantRounds:  2,
		},
		{
			name: "a released review holds nothing",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.releaseFirstReview(t)
			},
			wantOrdinal: 2,
			wantRounds:  2,
		},
		{
			name: "a rebase part way through reads the newest round",
			arrange: func(_ *testing.T, fx *reviewFixture) {
				fx.snapshots.Err = fail.Precondition("a rebase or merge is part way through")
			},
			wantOrdinal: 1,
			wantRounds:  1,
			wantNotice:  midSequenceNotice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			tt.arrange(t, fx)
			fx.snapshots.SHA = secondSnapshot

			got, err := fx.service.GetHistory(t.Context(), GetHistoryParams{
				Task:   *fx.writer.TaskID,
				Fresh:  tt.fresh,
				Author: personAuthor,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantOrdinal, got.Round.Ordinal)
			assert.Len(t, got.Rounds, tt.wantRounds)
			assert.Equal(t, tt.wantPinned, got.Pinned)
			assert.Equal(t, tt.wantNotice, got.Notice)
			assert.Equal(t, got.Round.SnapshotSHA, got.Span.HeadSHA, "the span is the round read")
		})
	}
}

// TestGetHistoryMeasuresFromTheLastReview covers the history read against the person's last review.
func TestGetHistoryMeasuresFromTheLastReview(t *testing.T) {
	fx := newReviewFixture(t)
	thread := fx.releaseFirstReview(t)
	fx.advanceTree(t)

	got, err := fx.service.GetHistory(t.Context(), GetHistoryParams{
		Task:   *fx.writer.TaskID,
		Author: personAuthor,
	})
	require.NoError(t, err)

	require.NotNil(t, got.Earlier)
	assert.Equal(t, int32(1), got.Earlier.Ordinal)
	assert.NotNil(t, got.EarlierReleasedAt)
	require.Len(t, got.Chapters, 2, "the reviewed round and the work since")
	assert.Equal(t, firstSnapshot, got.Chapters[0].ToSHA)
	assert.Equal(t, secondSnapshot, got.Chapters[1].ToSHA)
	require.Len(t, got.Threads, 1)
	assert.Equal(t, thread.ID, got.Threads[0].Thread.ID)
}

package reviews

import (
	"testing"

	"komrad/internal/fail"

	taskstore "komrad/internal/store/task"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetHistoryPicksTheSnapshot covers which snapshot the panel opens a review on.
func TestGetHistoryPicksTheSnapshot(t *testing.T) {
	tests := []struct {
		name          string
		arrange       func(t *testing.T, fx *reviewFixture)
		wantOrdinal   int32
		wantSnapshots int
		wantNotice    string
	}{
		{
			name:          "the tree as it stands is taken as a new snapshot",
			arrange:       func(*testing.T, *reviewFixture) {},
			wantOrdinal:   2,
			wantSnapshots: 2,
		},
		{
			name: "an open review holds nothing",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
			},
			wantOrdinal:   2,
			wantSnapshots: 2,
		},
		{
			name: "a released review holds nothing",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.releaseFirstReview(t)
			},
			wantOrdinal:   2,
			wantSnapshots: 2,
		},
		{
			name: "a rebase part way through reads the newest snapshot",
			arrange: func(_ *testing.T, fx *reviewFixture) {
				fx.snapshots.Err = fail.Precondition("a rebase or merge is part way through")
			},
			wantOrdinal:   1,
			wantSnapshots: 1,
			wantNotice:    midSequenceNotice,
		},
		{
			name: "a removed worktree reads the newest snapshot",
			arrange: func(t *testing.T, fx *reviewFixture) {
				require.NoError(t, fx.stores.Tasks.SetHead(t.Context(), *fx.writer.TaskID, taskstore.SetHeadParams{WorktreeRemoved: true}))
			},
			wantOrdinal:   1,
			wantSnapshots: 1,
			wantNotice:    worktreeRemovedNotice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			tt.arrange(t, fx)
			fx.snapshots.SHA = secondSnapshot

			got, err := fx.service.GetHistory(t.Context(), GetHistoryParams{
				Task:   *fx.writer.TaskID,
				Author: personAuthor,
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantOrdinal, got.Snapshot.Ordinal)
			assert.Len(t, got.Snapshots, tt.wantSnapshots)
			assert.Equal(t, tt.wantNotice, got.Notice)
			assert.Equal(t, got.Snapshot.SHA, got.Span.HeadSHA, "the span is the snapshot read")
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
	require.Len(t, got.Threads, 1)
	assert.Equal(t, thread.ID, got.Threads[0].Thread.ID)
}

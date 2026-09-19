package ui

import (
	"testing"

	"weave/internal/git"
	reviewstore "weave/internal/store/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewStateNoticesSayWhoseTurnItIs covers the eight the record spells out, one per state
// the column draws away from its baseline, each saying whose move is next.
func TestReviewStateNoticesSayWhoseTurnItIs(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want string
	}{
		{
			name: "approved on this tree",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved)},
			want: localApprovedNotice,
		},
		{
			name: "approved, tree moved since",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved), localTree: git.StandingMoved},
			want: localApprovedMovedNotice,
		},
		{
			name: "changes requested, agent still owes fixes",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested)},
			want: localChangesOwedNotice,
		},
		{
			name: "changes requested, agent pushed since",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested), localTree: git.StandingMoved},
			want: localChangesPushedNotice,
		},
		{
			name: "approved and landed",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved), localTree: git.StandingLanded},
			want: localApprovedLandedNotice,
		},
		{
			name: "approved and landed, new work since",
			item: agentItem{localVerdict: string(reviewstore.VerdictApproved), localTree: git.StandingLandedMoved},
			want: localApprovedLandedMovedNotice,
		},
		{
			name: "changes requested, landed as it was",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested), localTree: git.StandingLanded},
			want: localChangesLandedNotice,
		},
		{
			name: "changes requested, landed with new work",
			item: agentItem{localVerdict: string(reviewstore.VerdictChangesRequested), localTree: git.StandingLandedMoved},
			want: localChangesLandedMovedNotice,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notices := agentNotices(noticeRow(tt.item))
			require.Len(t, notices, 1, "the record says it once")
			assert.Equal(t, tt.want, notices[0].text)
		})
	}
}

// TestReviewStateNoticesLeaveNeverReviewedSilent pins the one baseline the record may leave
// unsaid, and that a comment without a conclusion is no review at all.
func TestReviewStateNoticesLeaveNeverReviewedSilent(t *testing.T) {
	assert.Empty(t, agentNotices(noticeRow(agentItem{})))
	assert.Empty(t, agentNotices(noticeRow(agentItem{localVerdict: string(reviewstore.VerdictCommented)})),
		"a review released without a conclusion says nothing")
	assert.Empty(t, agentNotices(noticeRow(agentItem{localTree: git.StandingMoved})),
		"a tree that moved with nothing concluded on it is still never reviewed")
}

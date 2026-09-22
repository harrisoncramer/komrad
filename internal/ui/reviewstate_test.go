package ui

import (
	"testing"

	"weave/internal/git"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewStateNoticesSayWhoseTurnItIs covers the eight the record spells out, one per state
// the column draws away from its baseline, each saying whose move is next.
func TestReviewStateNoticesSayWhoseTurnItIs(t *testing.T) {
	tests := []struct {
		name string
		item panel.AgentRow
		want string
	}{
		{
			name: "approved on this tree",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED},
			want: localApprovedNotice,
		},
		{
			name: "approved, tree moved since",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, LocalTree: git.StandingMoved},
			want: localApprovedMovedNotice,
		},
		{
			name: "changes requested, agent still owes fixes",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED},
			want: localChangesOwedNotice,
		},
		{
			name: "changes requested, agent pushed since",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, LocalTree: git.StandingMoved},
			want: localChangesPushedNotice,
		},
		{
			name: "approved and landed",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, LocalTree: git.StandingLanded},
			want: localApprovedLandedNotice,
		},
		{
			name: "approved and landed, new work since",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, LocalTree: git.StandingLandedMoved},
			want: localApprovedLandedMovedNotice,
		},
		{
			name: "changes requested, landed as it was",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, LocalTree: git.StandingLanded},
			want: localChangesLandedNotice,
		},
		{
			name: "changes requested, landed with new work",
			item: panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED, LocalTree: git.StandingLandedMoved},
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
	assert.Empty(t, agentNotices(noticeRow(panel.AgentRow{})))
	assert.Empty(t, agentNotices(noticeRow(panel.AgentRow{LocalVerdict: agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED})),
		"a review released without a conclusion says nothing")
	assert.Empty(t, agentNotices(noticeRow(panel.AgentRow{LocalTree: git.StandingMoved})),
		"a tree that moved with nothing concluded on it is still never reviewed")
}

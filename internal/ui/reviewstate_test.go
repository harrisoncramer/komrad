package ui

import (
	"testing"
	"weave/internal/ui/paint"

	reviewstore "weave/internal/store/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerdictMarkDrawsOnlyAConclusion is why commented carries no glyph: the row says what wants
// a person, and notes without a conclusion want nothing.
func TestVerdictMarkDrawsOnlyAConclusion(t *testing.T) {
	tests := []struct {
		name    string
		verdict reviewstore.Verdict
		icon    string
	}{
		{
			name:    "commented",
			verdict: reviewstore.VerdictCommented,
			icon:    "",
		},
		{
			name:    "approved",
			verdict: reviewstore.VerdictApproved,
			icon:    localApprovedIcon,
		},
		{
			name:    "changes requested",
			verdict: reviewstore.VerdictChangesRequested,
			icon:    localChangesRequestedIcon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mark := localReviewMark(paint.Panel, false, string(tt.verdict))
			if tt.icon == "" {
				assert.Empty(t, mark)

				return
			}
			assert.Contains(t, mark, tt.icon)
		})
	}

	assert.Empty(t, localReviewMark(paint.Panel, false, ""), "work no review has concluded on carries nothing")
}

// TestInReviewMarkSaysAReviewerIsOwedAnAnswer pins the glyph that separates work being read from
// work whose reading is over.
func TestInReviewMarkSaysAReviewerIsOwedAnAnswer(t *testing.T) {
	assert.Contains(t, localReviewMark(paint.Panel, true, ""), inLocalReviewIcon)
	assert.Empty(t, localReviewMark(paint.Panel, false, ""))
}

// TestReviewMarkGivesAVerdictTheColumn is why a sent-back round draws one glyph rather than two:
// a live reviewer is implied by every verdict short of approval, so the pencil beside it said
// nothing a person could act on.
func TestReviewMarkGivesAVerdictTheColumn(t *testing.T) {
	sentBack := localReviewMark(paint.Panel, true, string(reviewstore.VerdictChangesRequested))
	assert.Contains(t, sentBack, localChangesRequestedIcon)
	assert.NotContains(t, sentBack, inLocalReviewIcon)

	noted := localReviewMark(paint.Panel, true, string(reviewstore.VerdictCommented))
	assert.Contains(t, noted, inLocalReviewIcon, "notes without a conclusion leave the reading unfinished")

	notices := agentNotices(noticeRow(agentItem{inLocalReview: true, localVerdict: string(reviewstore.VerdictChangesRequested)}))
	require.Len(t, notices, 1, "the record says it once too")
	assert.Contains(t, notices[0].text, "asked for changes")
}

// TestReviewStateNoticesSayWhatTheGlyphsMean covers the two the record spells out, since a tick
// beside a name says nothing about what was ticked.
func TestReviewStateNoticesSayWhatTheGlyphsMean(t *testing.T) {
	reading := agentNotices(noticeRow(agentItem{inLocalReview: true}))
	require.Len(t, reading, 1)
	assert.Contains(t, reading[0].text, "in review")

	approved := agentNotices(noticeRow(agentItem{localVerdict: string(reviewstore.VerdictApproved)}))
	require.Len(t, approved, 1)
	assert.Contains(t, approved[0].text, "approved")

	changes := agentNotices(noticeRow(agentItem{localVerdict: string(reviewstore.VerdictChangesRequested)}))
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].text, "asked for changes")

	assert.Empty(t, agentNotices(noticeRow(agentItem{localVerdict: string(reviewstore.VerdictCommented)})),
		"a review released without a conclusion says nothing")
}

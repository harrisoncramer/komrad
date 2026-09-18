package ui

import (
	"testing"

	reviewstore "weave/internal/store/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerdictIconDrawsOnlyAConclusion is why commented carries no glyph: the record says what
// wants a person, and notes without a conclusion want nothing.
func TestVerdictIconDrawsOnlyAConclusion(t *testing.T) {
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
			icon, _ := localVerdictIcon(string(tt.verdict))
			assert.Equal(t, tt.icon, icon)
		})
	}

	icon, _ := localVerdictIcon("")
	assert.Empty(t, icon, "work no review has concluded on carries nothing")
}

// TestReviewNoticeSaysTheVerdictOnce pins the record spelling a sent-back round out, which is
// the only place a verdict is said now the row draws no glyph for it.
func TestReviewNoticeSaysTheVerdictOnce(t *testing.T) {
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

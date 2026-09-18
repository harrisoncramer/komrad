package ui

import (
	"testing"
	"weave/internal/ui/paint"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewRoundsMarkCarriesTheCount pins the number beside the glyph.
func TestReviewRoundsMarkCarriesTheCount(t *testing.T) {
	assert.Empty(t, localReviewRoundsMark(paint.Colored, 0), "an agent no review has touched carries nothing")
	assert.Contains(t, localReviewRoundsMark(paint.Colored, 4), localReviewRoundsIcon)
	assert.Contains(t, localReviewRoundsMark(paint.Colored, 4), "4")
}

// TestReviewRoundsNoticeSaysWhatTheGlyphMeans is why the record spells the count out: the glyph
// says a number without saying what was counted.
func TestReviewRoundsNoticeSaysWhatTheGlyphMeans(t *testing.T) {
	notices := agentNotices(agentItem{localRounds: 3})

	require.Len(t, notices, 1)
	assert.Contains(t, notices[0].text, "reviewed 3 times")
	assert.Contains(t, agentNotices(agentItem{localRounds: 1})[0].text, "reviewed once", "one round is not 1 times")
	assert.Empty(t, agentNotices(agentItem{}), "an agent no review has touched says nothing")
}

// TestReviewRoundsNoticeIsGivenUpFirst pins where the count sits in the order: a short pane
// drops the notices at the tail, and a count of rounds already gone by is the one worth losing.
func TestReviewRoundsNoticeIsGivenUpFirst(t *testing.T) {
	notices := agentNotices(agentItem{
		agent:       agentstore.Agent{Hidden: true},
		unreadMail:  true,
		localRounds: 2,
	})

	require.Len(t, notices, 3)
	assert.Contains(t, notices[len(notices)-1].text, "reviewed 2 times")
}

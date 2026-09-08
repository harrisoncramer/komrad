package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerdictMarkDrawsOnlyAConclusion is why commented carries no glyph: the row says what wants
// a person, and notes without a conclusion want nothing.
func TestVerdictMarkDrawsOnlyAConclusion(t *testing.T) {
	for _, tt := range []struct {
		name    string
		verdict reviewstore.Verdict
		icon    string
	}{
		{name: "approved", verdict: reviewstore.VerdictApproved, icon: approvedIcon},
		{name: "changes requested", verdict: reviewstore.VerdictChangesRequested, icon: changesRequestedIcon},
		{name: "commented", verdict: reviewstore.VerdictCommented},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mark := verdictMark(string(tt.verdict))
			if tt.icon == "" {
				assert.Empty(t, mark)

				return
			}
			assert.Contains(t, mark, tt.icon)
		})
	}

	assert.Empty(t, verdictMark(""), "work no review has concluded on carries nothing")
}

// TestInReviewMarkSaysAReviewerIsOwedAnAnswer pins the glyph that separates work being read from
// work whose reading is over.
func TestInReviewMarkSaysAReviewerIsOwedAnAnswer(t *testing.T) {
	assert.Contains(t, inReviewMark(true), inReviewIcon)
	assert.Empty(t, inReviewMark(false))
}

// TestReviewStateNoticesSayWhatTheGlyphsMean covers the two the record spells out, since a tick
// beside a name says nothing about what was ticked.
func TestReviewStateNoticesSayWhatTheGlyphsMean(t *testing.T) {
	reading := agentNotices(agentItem{inReview: true})
	require.Len(t, reading, 1)
	assert.Contains(t, reading[0].text, "in review")

	approved := agentNotices(agentItem{verdict: string(reviewstore.VerdictApproved)})
	require.Len(t, approved, 1)
	assert.Contains(t, approved[0].text, "approved")

	changes := agentNotices(agentItem{verdict: string(reviewstore.VerdictChangesRequested)})
	require.Len(t, changes, 1)
	assert.Contains(t, changes[0].text, "changes requested")

	assert.Empty(t, agentNotices(agentItem{verdict: string(reviewstore.VerdictCommented)}),
		"a review released without a conclusion says nothing")
}

// TestAgentsViewReviewState pins the glyphs drawn per agent, so the three places a change can
// stand are told apart on the screen a person decides from.
func TestAgentsViewReviewState(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "a1", Name: "being-read", WorkflowID: "w1"}, inReview: true},
			{agent: agentstore.Agent{ID: "a2", Name: "signed-off", WorkflowID: "w1"}, verdict: string(reviewstore.VerdictApproved)},
			{agent: agentstore.Agent{ID: "a3", Name: "sent-back", WorkflowID: "w1"}, verdict: string(reviewstore.VerdictChangesRequested)},
			{agent: agentstore.Agent{ID: "a4", Name: "unread", WorkflowID: "w1"}},
		}},
	}

	rows := strings.Split(m.agentsView(), "\n")
	for _, tt := range []struct {
		name string
		icon string
	}{
		{name: "being-read", icon: inReviewIcon},
		{name: "signed-off", icon: approvedIcon},
		{name: "sent-back", icon: changesRequestedIcon},
	} {
		row, ok := rowNamed(rows, tt.name)
		require.True(t, ok, tt.name)
		assert.Contains(t, row, tt.icon)
	}

	fresh, ok := rowNamed(rows, "unread")
	require.True(t, ok)
	for _, icon := range []string{inReviewIcon, approvedIcon, changesRequestedIcon} {
		assert.NotContains(t, fresh, icon)
	}
}

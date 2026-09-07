package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewRoundsMarkCarriesTheCount pins the number beside the glyph, since a loop is told
// from an ordinary review by how far the count has run rather than by the mark being there.
func TestReviewRoundsMarkCarriesTheCount(t *testing.T) {
	assert.Empty(t, reviewRoundsMark(0), "an agent no review has touched carries nothing")
	assert.Contains(t, reviewRoundsMark(4), reviewRoundsIcon)
	assert.Contains(t, reviewRoundsMark(4), "4")
}

// TestReviewRoundsMarkAddsBothDirections covers the reviewer and the agent it reads for wearing
// the same count, since one agent is only ever on one side of a round.
func TestReviewRoundsMarkAddsBothDirections(t *testing.T) {
	assert.Equal(t, int32(3), reviewRounds(agentItem{roundsReceived: 3}))
	assert.Equal(t, int32(3), reviewRounds(agentItem{roundsSent: 3}))
}

// TestReviewRoundsNoticeSaysWhichWay is why the record spells the count out: the row adds the
// two directions up, and which of them ran away is the difference between a loop and a reviewer
// that has read a lot.
func TestReviewRoundsNoticeSaysWhichWay(t *testing.T) {
	notices := agentNotices(agentItem{roundsReceived: 3, roundsSent: 1})

	require.Len(t, notices, 1)
	assert.Contains(t, notices[0].text, "3 given on its work")
	assert.Contains(t, notices[0].text, "1 it has given")
	assert.Empty(t, agentNotices(agentItem{}), "an agent no review has touched says nothing")
}

// TestReviewRoundsNoticeIsGivenUpFirst pins where the count sits in the order: a short pane drops
// the notices at the tail, and a count of rounds already gone by is the one worth losing, since
// every other mark stands for something a person can still act on.
func TestReviewRoundsNoticeIsGivenUpFirst(t *testing.T) {
	notices := agentNotices(agentItem{
		agent:          agentstore.Agent{Hidden: true},
		unreadMail:     true,
		roundsReceived: 2,
	})

	require.Len(t, notices, 3)
	assert.Contains(t, notices[len(notices)-1].text, "review rounds")
}

// TestAgentsViewReviewRounds pins the glyph drawn per agent, so one that has been round several
// times shows it and the one beside it does not.
func TestAgentsViewReviewRounds(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "going-round",
				WorkflowID: "w1",
			}, roundsReceived: 4},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "first-pass",
				WorkflowID: "w1",
			}},
		}},
	}

	rows := strings.Split(m.agentsView(), "\n")
	looping, ok := rowNamed(rows, "going-round")
	require.True(t, ok)
	assert.Contains(t, looping, reviewRoundsIcon)
	assert.Contains(t, looping, "4")

	fresh, ok := rowNamed(rows, "first-pass")
	require.True(t, ok)
	assert.NotContains(t, fresh, reviewRoundsIcon)
}

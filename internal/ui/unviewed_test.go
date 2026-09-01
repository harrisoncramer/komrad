package ui

import (
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unviewedTree() model {
	tmux := &recordingTmux{}
	popup := NewPopupWindow(tmux, "test-session")
	popup.MarkOpen()

	return model{
		frame: layout.New(tmux, config.DefaultLayout()),
		popup: popup,
		keys:  config.DefaultKeybindings(),
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						ID:         "a1",
						Name:       "auth",
						WorkflowID: "w1",
					},
					alive:    true,
					unviewed: true,
				},
				{agent: agentstore.Agent{
					ID:         "a2",
					Name:       "api",
					WorkflowID: "w1",
				}, alive: true},
				{
					agent: agentstore.Agent{
						ID:         "a3",
						Name:       "db",
						WorkflowID: "w1",
					},
					alive:    true,
					unviewed: true,
				},
			},
		},
	}
}

// TestDwellViewedWaitsForTheCursorToSettle covers the case the whole rule exists for: the panel
// stages a live agent's own pane on hover, so travelling down the list would otherwise mark
// every row a person passed on the way to the one they wanted.
func TestDwellViewedWaitsForTheCursorToSettle(t *testing.T) {
	at := time.Now()

	m := unviewedTree()
	m.tree.cursor = 1

	m, cmd := m.dwellViewed(at)
	assert.Nil(t, cmd, "the tick the cursor arrived on marks nothing")

	m, cmd = m.dwellViewed(at.Add(viewedDwell / 2))
	assert.Nil(t, cmd, "still inside the dwell")

	_, cmd = m.dwellViewed(at.Add(viewedDwell))
	assert.NotNil(t, cmd, "the pane has been on screen long enough to have been read")
}

// TestDwellViewedRestartsWhenTheCursorMoves pins that the clock belongs to the row rather than
// to the panel, so a walk down the list never accumulates a mark for the row it stops on.
func TestDwellViewedRestartsWhenTheCursorMoves(t *testing.T) {
	at := time.Now()

	m := unviewedTree()
	m.tree.cursor = 1

	m, _ = m.dwellViewed(at)

	m.tree.cursor = 3
	m, cmd := m.dwellViewed(at.Add(viewedDwell))
	assert.Nil(t, cmd, "the cursor only just landed here")

	_, cmd = m.dwellViewed(at.Add(2 * viewedDwell))
	assert.NotNil(t, cmd)
}

// TestDwellViewedSkipsRowsThatShowNoPane covers the rows a dwell says nothing about: a header,
// a row already read, and a dormant agent, which stages a placeholder rather than its output.
func TestDwellViewedSkipsRowsThatShowNoPane(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		screen screen
		mutate func(*model)
	}{
		{name: "a workflow header", cursor: 0},
		{name: "an agent already read", cursor: 2},
		{
			name:   "a dormant agent",
			cursor: 1,
			mutate: func(m *model) { m.listing.agents[0].alive = false },
		},
		{
			name:   "another screen is up",
			cursor: 1,
			screen: screenSchedules,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			at := time.Now()

			m := unviewedTree()
			m.tree.cursor = tt.cursor
			m.screen = tt.screen
			if tt.mutate != nil {
				tt.mutate(&m)
			}

			m, _ = m.dwellViewed(at)
			_, cmd := m.dwellViewed(at.Add(2 * viewedDwell))
			assert.Nil(t, cmd)
		})
	}
}

// TestToggleViewedHoldsAgainstTheDwell covers the point of a manual mark: it is set while the
// cursor is on the row, which is the one place the dwell would undo it a second later.
func TestToggleViewedHoldsAgainstTheDwell(t *testing.T) {
	at := time.Now()

	m := unviewedTree()
	m.tree.cursor = 2

	// The row was read, and the person puts the mark back by hand.
	item, ok := m.selectedAgent()
	require.True(t, ok)
	require.False(t, item.unviewed)

	next, _ := m.toggleAgentViewed(item)
	m, ok = next.(model)
	require.True(t, ok)
	require.True(t, m.hover.held)

	// The write lands and the row comes back unviewed, which is when the dwell would pounce.
	m.listing.agents[1].unviewed = true

	m, cmd := m.dwellViewed(at)
	assert.Nil(t, cmd)

	_, cmd = m.dwellViewed(at.Add(10 * viewedDwell))
	assert.Nil(t, cmd, "a mark set by hand stands while the cursor is still on the row")
}

// TestToggleViewedLetsGoWhenTheCursorMoves pins that the hold is for as long as you are looking
// at the row and no longer, so coming back to it later reads it the way anything else does.
func TestToggleViewedLetsGoWhenTheCursorMoves(t *testing.T) {
	at := time.Now()

	m := unviewedTree()
	m.tree.cursor = 2

	item, ok := m.selectedAgent()
	require.True(t, ok)

	next, _ := m.toggleAgentViewed(item)
	m, ok = next.(model)
	require.True(t, ok)
	m.listing.agents[1].unviewed = true

	m.tree.cursor = 1
	m, _ = m.dwellViewed(at)
	m.tree.cursor = 2

	m, cmd := m.dwellViewed(at.Add(time.Second))
	assert.Nil(t, cmd, "the cursor only just came back")

	_, cmd = m.dwellViewed(at.Add(time.Second).Add(viewedDwell))
	assert.NotNil(t, cmd)
}

// TestJumpUnviewed covers the half of this that makes the dwell safe: reaching an agent without
// travelling over the rows in between.
func TestJumpUnviewed(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		key    string
		want   int
	}{
		{
			name: "from the header to the first unread",
			key:  "n",
			want: 1,
		},
		{
			name:   "past the read row to the next unread",
			cursor: 1,
			key:    "n",
			want:   3,
		},
		{
			name:   "wrapping back to the first",
			cursor: 3,
			key:    "n",
			want:   1,
		},
		{
			name:   "backwards to the previous unread",
			cursor: 3,
			key:    "N",
			want:   1,
		},
		{
			name:   "backwards wrapping to the last",
			cursor: 1,
			key:    "N",
			want:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := unviewedTree()
			m.tree.cursor = tt.cursor

			assert.Equal(t, tt.want, press(t, m, keyMsg(tt.key)).tree.cursor)
		})
	}
}

// TestJumpUnviewedStaysPutWithNothingToReach pins that the key does nothing rather than moving
// the cursor somewhere arbitrary when every agent has been read.
func TestJumpUnviewedStaysPutWithNothingToReach(t *testing.T) {
	m := unviewedTree()
	for i := range m.listing.agents {
		m.listing.agents[i].unviewed = false
	}
	m.tree.cursor = 2

	assert.Equal(t, 2, press(t, m, keyMsg("n")).tree.cursor)
}

// TestUnviewedMarkIsItsOwnColumn pins the separation the whole design rests on: the state icon
// says only what the agent is doing, the mark beside it says only whether there is something to
// read, and either is true without the other.
func TestUnviewedMarkIsItsOwnColumn(t *testing.T) {
	assert.Contains(t, unviewedMark(true), unviewedIcon)
	assert.Empty(t, unviewedMark(false), "and takes no room on a row with nothing to read")
}

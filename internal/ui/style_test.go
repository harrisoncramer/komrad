package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// branchName is the kind of name an agent spawned off a ticket carries: the branch it
// works on, which is longer than a row has space for.
const branchName = "eng-5433-turn-on-crm-integrations-for-teen-for-america-ein-46-0825676"

// TestClipName covers the cut itself. A name that fits is drawn as it came, and one that
// does not ends in an ellipsis inside the same width, so no row is wider than another.
func TestClipName(t *testing.T) {
	assert.Equal(t, "auth", clipName("auth"))

	clipped := clipName(branchName)
	assert.Len(t, []rune(clipped), nameWidth)
	assert.True(t, strings.HasSuffix(clipped, "…"), "a clipped name says it was cut: %q", clipped)
	assert.True(t, strings.HasPrefix(branchName, strings.TrimSuffix(clipped, "…")),
		"a clipped name is the head of the whole one: %q", clipped)
}

// branchTree is one agent named after its branch, which is the case the clip is for.
func branchTree() model {
	return model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "chariot"}}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "a1", Name: branchName, WorkflowID: "w1"}},
		}},
	}
}

// TestAgentsViewClipsName covers the rendered screen. The head of the name is on it and
// the tail is not, and the whole name is still what the filter reads, so a row matches on
// a word the list never drew.
func TestAgentsViewClipsName(t *testing.T) {
	m := branchTree()

	view := m.agentsView()
	assert.Contains(t, view, "eng-5433-turn-on-crm")
	assert.NotContains(t, view, "0825676")

	m.filter.setQuery("0825676")
	assert.Len(t, m.agentRows(), 2, "the workflow header and the row that matched")
}

func TestUnreadMailMark(t *testing.T) {
	assert.Empty(t, unreadMailMark(false))
	assert.Contains(t, unreadMailMark(true), unreadMailIcon)
}

// TestAgentsViewUnreadMail covers the rendered row. The glyph is observed per agent at
// read time rather than stored, so one agent with mail waiting draws it and the other
// beside it, with none, does not.
func TestAgentsViewUnreadMail(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "chariot"}}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "a1", Name: "waiting-on-reply", WorkflowID: "w1"}, unreadMail: true},
			{agent: agentstore.Agent{ID: "a2", Name: "caught-up", WorkflowID: "w1"}, unreadMail: false},
		}},
	}

	rows := strings.Split(m.agentsView(), "\n")
	waiting, ok := rowNamed(rows, "waiting-on-reply")
	require.True(t, ok)
	assert.Contains(t, waiting, unreadMailIcon)

	caughtUp, ok := rowNamed(rows, "caught-up")
	require.True(t, ok)
	assert.NotContains(t, caughtUp, unreadMailIcon)
}

// rowNamed returns the rendered row containing name, so a test can check what is drawn
// beside one agent without depending on where it lands among the others.
func rowNamed(rows []string, name string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row, true
		}
	}

	return "", false
}

// TestFocusColours covers the accent moving between the two halves of the sidebar. Only one
// of them has the keys, and the colour is what says which: the half that has them draws its
// title and its cursor in the accent, and the other keeps both in place without it, its title
// grey and its cursor in the terminal's own foreground.
func TestFocusColours(t *testing.T) {
	m := model{}

	assert.Equal(t, colorAccent, m.titleStyle().GetForeground(), "the list has the keys to begin with")
	assert.Equal(t, colorAccent, m.rowCursorStyle().GetForeground())
	assert.Equal(t, colorMuted, titleStyleFor(false).GetForeground(), "the details title is grey under it")
	assert.Empty(t, cursorStyleFor(false).GetForeground(), "and its cursor names no colour at all")

	m.details.focused = true

	assert.Equal(t, colorMuted, m.titleStyle().GetForeground(), "the accent goes with the keys")
	assert.Empty(t, m.rowCursorStyle().GetForeground())
	assert.Equal(t, colorAccent, titleStyleFor(true).GetForeground())
	assert.Equal(t, colorAccent, cursorStyleFor(true).GetForeground())
}

// TestFocusKeepsTheMarkers covers what does not change with the focus: both cursors stay
// drawn, so which row a record is about, and which field the keys would copy, are on screen
// whichever half is being typed at.
func TestFocusKeepsTheMarkers(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.Contains(t, m.detailsView(), "›", "the field cursor is drawn while the list has the keys")

	m.details.focused = true

	assert.Contains(t, m.agentsView(), "›", "and the row cursor while the details have them")
}

package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBranchMark covers the git branch shown beside an agent's row: nothing for one the
// branch syncer has not found a branch for, and the branch itself, icon included, for one
// it has.
func TestBranchMark(t *testing.T) {
	tests := []struct {
		name   string
		branch *string
		want   string
	}{
		{
			name: "no branch found is drawn as nothing",
		},
		{
			name:   "a found branch is drawn with its icon",
			branch: xptr.Ptr("feature/worktrees"),
			want:   " " + branchIcon + " feature/worktrees",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := branchMark(agentstore.Agent{Branch: tt.branch})
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.want)
		})
	}
}

// TestAgentsViewShowsBranch covers the row itself: an agent the branch syncer found a
// branch for shows it beside its name, and one it has not draws the row without it.
func TestAgentsViewShowsBranch(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "chariot"}}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "a1", Name: "worker", WorkflowID: "w1", Branch: xptr.Ptr("agent/a1")}},
			{agent: agentstore.Agent{ID: "a2", Name: "reviewer", WorkflowID: "w1"}},
		}},
	}

	view := m.agentsView()
	assert.Contains(t, view, "worker")
	assert.Contains(t, view, branchIcon+" agent/a1")
	assert.Contains(t, view, "reviewer")
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

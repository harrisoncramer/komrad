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

// TestAgentsViewRowWidthIsStableRegardlessOfBranch covers the fixed slot left where the
// branch glyph used to draw: an agent with a branch and one without must produce rows of
// the same width, so nothing jitters as the cwd syncer catches up.
func TestAgentsViewRowWidthIsStableRegardlessOfBranch(t *testing.T) {
	withBranch := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "worker",
			WorkflowID: "w1",
			Branch:     xptr.Ptr("agent/a1"),
		}}}},
	}
	withoutBranch := withBranch
	withoutBranch.listing.agents = []agentItem{{agent: agentstore.Agent{
		ID:         "a1",
		Name:       "worker",
		WorkflowID: "w1",
	}}}

	rowWithBranch, ok := rowNamed(strings.Split(withBranch.agentsView(), "\n"), "worker")
	require.True(t, ok)
	assert.NotContains(t, rowWithBranch, "agent/a1", "the branch name itself is never drawn")

	rowWithoutBranch, ok := rowNamed(strings.Split(withoutBranch.agentsView(), "\n"), "worker")
	require.True(t, ok)

	assert.Len(t, rowWithoutBranch, len(rowWithBranch))
}

// TestUncertainStateMark covers the row weave draws for a pane it captured and could not read,
// which is its own glyph rather than the idle dot it used to be collapsed into.
func TestUncertainStateMark(t *testing.T) {
	assert.Equal(t, "?", stateIcon(agentstore.StateUncertain))
	assert.NotEqual(t, stateIcon(agentstore.StateIdle), stateIcon(agentstore.StateUncertain))
	assert.Equal(t, colorMuted, stateColor(agentstore.StateUncertain), "an unreadable pane is quiet, not an alarm")
}

// TestWorktreeRemovedMark covers the glyph shown beside an agent's row when the worktree
// syncer has found its cwd gone: nothing for one whose cwd is still there, and just the
// icon for one it is not.
func TestWorktreeRemovedMark(t *testing.T) {
	tests := []struct {
		name    string
		removed bool
		want    bool
	}{
		{
			name: "a cwd still on disk is drawn as nothing",
		},
		{
			name:    "a cwd the syncer found gone is drawn with the icon",
			removed: true,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worktreeRemovedMark(agentstore.Agent{WorktreeRemoved: tt.removed})
			if !tt.want {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, worktreeRemovedIcon)
		})
	}
}

// TestAgentsViewShowsWorktreeRemoved covers the row itself: an agent the cwd syncer
// found gone shows the icon beside its name, and one it has not draws the row without it.
func TestAgentsViewShowsWorktreeRemoved(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:              "a1",
				Name:            "worker",
				WorkflowID:      "w1",
				WorktreeRemoved: true,
			}},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "reviewer",
				WorkflowID: "w1",
			}},
		}},
	}

	rows := strings.Split(m.agentsView(), "\n")
	worker, ok := rowNamed(rows, "worker")
	require.True(t, ok)
	assert.Contains(t, worker, worktreeRemovedIcon)

	reviewer, ok := rowNamed(rows, "reviewer")
	require.True(t, ok)
	assert.NotContains(t, reviewer, worktreeRemovedIcon)
}

// TestUnreadMailMark pins that the glyph is drawn only for an agent with mail waiting.
func TestUnreadMailMark(t *testing.T) {
	assert.Empty(t, unreadMailMark(false))
	assert.Contains(t, unreadMailMark(true), unreadMailIcon)
}

// TestAgentsViewUnreadMail pins the mail glyph drawn per agent, so one with mail waiting
// shows it and the one beside it does not.
func TestAgentsViewUnreadMail(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "waiting-on-reply",
				WorkflowID: "w1",
			}, unreadMail: true},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "caught-up",
				WorkflowID: "w1",
			}, unreadMail: false},
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

func rowNamed(rows []string, name string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row, true
		}
	}

	return "", false
}

// TestFocusColours pins the accent marking which half of the sidebar has the keys, while the
// other keeps its title grey and its cursor in the terminal's own foreground.
func TestFocusColours(t *testing.T) {
	m := detailsModel(&recordingTmux{})

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

package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
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
	assert.Equal(t, paint.ToneMuted, stateTone(agentstore.StateUncertain), "an unreadable pane is quiet, not an alarm")
}

// TestWorktreeMark covers the one glyph the row spends on the state of a checkout, which
// says a cwd is gone, a sequence is stopped in it, or nothing at all.
func TestWorktreeMark(t *testing.T) {
	tests := []struct {
		name    string
		agent   agentstore.Agent
		wantOne string
	}{
		{
			name: "a settled checkout is drawn as nothing",
		},
		{
			name:    "a cwd the syncer found gone is drawn with the removed icon",
			agent:   agentstore.Agent{WorktreeRemoved: true},
			wantOne: worktreeRemovedIcon,
		},
		{
			name:    "a checkout part-way through a rebase is drawn with the sequence icon",
			agent:   agentstore.Agent{SequenceInProgress: true},
			wantOne: sequenceInProgressIcon,
		},
		{
			name:    "a cwd that is gone outranks a sequence nothing can be doing in it",
			agent:   agentstore.Agent{WorktreeRemoved: true, SequenceInProgress: true},
			wantOne: worktreeRemovedIcon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worktreeMark(paint.Panel, agentItem{agent: tt.agent})
			if tt.wantOne == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.wantOne)
		})
	}
}

// TestUnreadMailMark pins that the glyph is drawn only for an agent with mail waiting.
func TestUnreadMailMark(t *testing.T) {
	assert.Empty(t, unreadMailMark(paint.Panel, false))
	assert.Contains(t, unreadMailMark(paint.Panel, true), unreadMailIcon)
}

func rowNamed(rows []string, name string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row, true
		}
	}

	return "", false
}

// TestFocusKeepsTheMarkers covers both cursors staying drawn, so which row a record is about,
// and which field the keys would copy, are on screen whichever half is being typed at.
func TestFocusKeepsTheMarkers(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.Contains(t, m.detailsView(), "›", "the field cursor is drawn while the list has the keys")

	m.details.focused = true

	assert.Contains(t, m.agentsView(), "›", "and the row cursor while the details have them")
}

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

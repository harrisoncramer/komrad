package ui

import (
	"strings"
	"testing"

	"weave/config"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/lib/xptr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// familyTree is one workflow holding two agents, the first of which has two children, so the
// order the rows come out in says both where a child goes and where it does not.
func familyTree() model {
	return model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		width: 60,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{
				{agent: agentstore.Agent{
					ID:         "a1",
					Name:       "author",
					WorkflowID: "w1",
					Cwd:        "/tmp/tree",
				}},
				{agent: agentstore.Agent{
					ID:         "a2",
					Name:       "fix-gutter",
					WorkflowID: "w1",
				}},
				{agent: agentstore.Agent{
					ID:            "a3",
					Name:          "reviewer",
					WorkflowID:    "w1",
					ParentAgentID: xptr.Ptr("a1"),
				}},
				{agent: agentstore.Agent{
					ID:            "a4",
					Name:          "test-runner",
					WorkflowID:    "w1",
					ParentAgentID: xptr.Ptr("a1"),
				}},
			},
		},
	}
}

// TestChildRowsFollowTheirParent pins that a child is drawn under the agent it is attached to
// rather than wherever its own id lands in the listing.
func TestChildRowsFollowTheirParent(t *testing.T) {
	rows := familyTree().agentRows()

	assert.Equal(t, []string{
		"header:weave",
		"agent:author",
		"agent:reviewer",
		"agent:test-runner",
		"agent:fix-gutter",
	}, describeRows(rows))

	assert.False(t, rows[1].child, "the parent is not indented")
	assert.True(t, rows[2].child)
	assert.True(t, rows[3].child)
	assert.False(t, rows[4].child)
}

// TestHiddenParentTakesItsChildren pins that hiding a parent hides the rows under it, since a
// child indented under nothing reads as belonging to whatever is above it.
func TestHiddenParentTakesItsChildren(t *testing.T) {
	m := familyTree()
	m.listing.agents[0].agent.Hidden = true

	rows := m.agentRows()
	assert.Equal(t, []string{"header:weave", "agent:fix-gutter"}, describeRows(rows))
	assert.Equal(t, 3, rows[0].hiddenCount, "the tally stands in for the whole family")

	m.tree.showHidden = true
	assert.Equal(t, []string{
		"header:weave",
		"agent:author",
		"agent:reviewer",
		"agent:test-runner",
		"agent:fix-gutter",
	}, describeRows(m.agentRows()))
}

// TestOrphanedChildStandsOnItsOwn pins that a child whose parent is not in the listing is still
// drawn, since a row that vanishes is worse than one indented under nothing.
func TestOrphanedChildStandsOnItsOwn(t *testing.T) {
	m := familyTree()
	m.listing.agents = m.listing.agents[1:]

	rows := m.agentRows()
	assert.Equal(t, []string{
		"header:weave",
		"agent:fix-gutter",
		"agent:reviewer",
		"agent:test-runner",
	}, describeRows(rows))
	assert.False(t, rows[2].child, "with nothing to indent under, it is a row like any other")
}

// TestAgentsViewIndentsAChild pins the drawn row, where the branch glyph sits in the column the
// parent's own state mark is in so that every parent stays aligned with the others.
func TestAgentsViewIndentsAChild(t *testing.T) {
	m := familyTree()
	m.height = 20

	view := ansi.Strip(m.agentsView())

	var author, reviewer string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "author") {
			author = line
		}
		if strings.Contains(line, "reviewer") {
			reviewer = line
		}
	}

	require.NotEmpty(t, author)
	require.NotEmpty(t, reviewer)
	assert.Equal(t, strings.Index(author, "•"), strings.Index(reviewer, childIcon),
		"the branch glyph takes the parent's state column")
	assert.Greater(t, strings.Index(reviewer, "reviewer"), strings.Index(author, "author"),
		"and the name sits two columns further in")
}

// TestChildIsNotReviewable pins that the review key answers on the parent's row only, since the
// diff in a child's directory is the parent's work and the threads on it are the parent's.
func TestChildIsNotReviewable(t *testing.T) {
	m := familyTree()
	m.listing.workflows[0].Worktree = workflowstore.WorktreeConfig{Enabled: true}

	assert.True(t, m.isReviewable(m.listing.agents[0].agent), "the parent's own worktree is reviewable")
	assert.False(t, m.isReviewable(m.listing.agents[2].agent))
}

// TestChildDetailsLeadWithTheParent pins what the details say about a child under its own name,
// since its directory makes no sense without the agent it belongs to.
func TestChildDetailsLeadWithTheParent(t *testing.T) {
	m := familyTree()
	m.tree.cursor = 2

	held := m.agentsDetails()
	require.Greater(t, len(held.fields), 1)
	assert.Equal(t, "name", held.fields[0].label)
	assert.Equal(t, "attached to", held.fields[1].label)
	assert.Equal(t, "author", held.fields[1].value)
}

// TestPromptSubjectCountsTheChildren pins that a prompt says what goes with the agent named,
// since the children go whether or not anybody asked about them.
func TestPromptSubjectCountsTheChildren(t *testing.T) {
	tests := []struct {
		name     string
		agents   []agentstore.Agent
		children int
		want     string
	}{
		{
			name:   "one agent with nothing attached",
			agents: []agentstore.Agent{{Name: "author"}},
			want:   `agent "author"`,
		},
		{
			name:     "one agent with one child",
			agents:   []agentstore.Agent{{Name: "author"}},
			children: 1,
			want:     `agent "author" and its child`,
		},
		{
			name:     "one agent with several",
			agents:   []agentstore.Agent{{Name: "author"}},
			children: 2,
			want:     `agent "author" and its 2 children`,
		},
		{
			name:     "a selection of agents",
			agents:   []agentstore.Agent{{Name: "author"}, {Name: "fix-gutter"}},
			children: 2,
			want:     "2 agents and 2 children",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, promptSubject(tt.agents, tt.children))
		})
	}
}

// TestDeletePromptSaysWhatGoesWithIt pins the hard delete asking about the children too, since
// it is the prompt with nothing to undo it and the archive prompt already says so.
func TestDeletePromptSaysWhatGoesWithIt(t *testing.T) {
	form, _ := deleteAgentsForm([]agentstore.Agent{{ID: "a1", Name: "author"}}, 2, config.DefaultKeybindings().Form)
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	view := ansi.Strip(form.View())
	assert.Contains(t, view, `Permanently delete agent "author" and its 2 children?`)
	assert.Contains(t, view, "Their transcripts go with them")
}

// TestUnnamedChildren pins the count the prompt is given, which leaves out a child the caller
// already named and another parent's children entirely.
func TestUnnamedChildren(t *testing.T) {
	pool := familyTree().liveAgents()

	assert.Equal(t, 2, unnamedChildren(pool, []agentstore.Agent{{ID: "a1"}}))
	assert.Equal(t, 1, unnamedChildren(pool, []agentstore.Agent{{ID: "a1"}, {ID: "a3", ParentAgentID: xptr.Ptr("a1")}}))
	assert.Equal(t, 0, unnamedChildren(pool, []agentstore.Agent{{ID: "a2"}}))
}

// TestUnnamedChildrenReadsTheArchivesOwnRows pins the count coming from the screen doing the
// asking, since the archive holds families the live listing does not.
func TestUnnamedChildrenReadsTheArchivesOwnRows(t *testing.T) {
	archived := []agentstore.Agent{
		{ID: "b1", Name: "old-author"},
		{
			ID:            "b2",
			Name:          "old-reviewer",
			ParentAgentID: xptr.Ptr("b1"),
		},
	}

	assert.Equal(t, 1, unnamedChildren(archived, []agentstore.Agent{{ID: "b1"}}))
	assert.Equal(t, 0, unnamedChildren(familyTree().liveAgents(), []agentstore.Agent{{ID: "b1"}}),
		"the live listing knows nothing about them")
}

// TestChildKeyOnAChildIsRefused pins that the key says why rather than opening a form whose
// submit the service could only refuse.
func TestChildKeyOnAChildIsRefused(t *testing.T) {
	m := familyTree()
	child := m.listing.agents[2].agent

	next, _ := m.openNewChild(child)
	refused, ok := next.(model)
	require.True(t, ok)

	require.NotEmpty(t, refused.messages.items)
	assert.Contains(t, newestMessage(refused).detail, "may not have children")
}

// TestHiddenChildCountsOnItsParent pins the tally landing on the row the child hangs off rather
// than on the workflow, since the parent is where a person went looking for it.
func TestHiddenChildCountsOnItsParent(t *testing.T) {
	m := familyTree()
	m.listing.agents[2].agent.Hidden = true

	rows := m.agentRows()
	assert.Equal(t, []string{
		"header:weave",
		"agent:author",
		"agent:test-runner",
		"agent:fix-gutter",
	}, describeRows(rows))
	assert.Equal(t, 0, rows[0].hiddenCount, "the workflow does not count it")
	assert.Equal(t, 1, rows[1].hiddenCount)

	m.height = 20
	view := ansi.Strip(m.agentsView())
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "author") {
			assert.Contains(t, line, "+1")
		}
	}

	m.tree.showHidden = true
	assert.NotContains(t, ansi.Strip(m.agentsView()), "+1")
}

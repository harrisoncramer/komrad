package ui

import (
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentsViewRowsOpenAtTheGutter pins how far in a row starts and that the gutter holds its
// columns on every kind of row, an agent opening on its checkout columns and a workflow, which
// owns no checkout, on the three blank cells those columns leave behind.
func TestAgentsViewRowsOpenAtTheGutter(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		width:  120,
		height: 40,
		listing: listing{
			workflows: []panel.Workflow{{
				ID:       "w1",
				Name:     "weave",
				Worktree: panel.WorktreeConfig{Enabled: true},
			}},
			agents: []agentItem{
				{
					agent: panel.Agent{
						ID:         "a1",
						Name:       "worker",
						WorkflowID: "w1",
						State:      panel.StateIdle,
					},
					alive: true,
				},
				{
					agent: panel.Agent{
						ID:         "o1",
						Name:       "orchestrator",
						WorkflowID: "w1",
						Role:       panel.RoleOrchestrator,
						State:      panel.StateIdle,
					},
					alive: true,
				},
			},
		},
	}
	m.agents.cursor = -1

	rows := strings.Split(m.agentsView(), "\n")
	// The focus and mark gutter cells, each a blank column and its separator.
	opening := 4

	agent, ok := rowNamed(rows, "worker")
	require.True(t, ok)
	stripped := ansi.Strip(agent)
	assert.Equal(t, opening, len(stripped)-len(strings.TrimLeft(stripped, " ")),
		"the worker row opens at the gutter")

	workflow, ok := rowNamed(rows, "weave")
	require.True(t, ok)
	assert.Equal(t, barColumn(t, agent), barColumn(t, workflow),
		"the bar stands in one column whatever the row is")
}

// quietBar is the bar an agent with nothing to say draws.
const quietBar = "│"

// barColumn is how far along a drawn row its status bar stands.
func barColumn(t *testing.T, row string) int {
	t.Helper()

	stripped := ansi.Strip(row)
	at := strings.Index(stripped, quietBar)
	require.GreaterOrEqual(t, at, 0, "the row draws no bar at all")

	return ansi.StringWidth(stripped[:at])
}

// agentsSortModel is the agents list over two workflows, the first holding three families whose
// created, activity and name orders all differ, one of them with two children and one named in
// capitals.
func agentsSortModel() model {
	at := func(minutes int) time.Time {
		return time.Date(2026, 9, 20, 12, minutes, 0, 0, time.UTC)
	}
	alpha := "alpha"

	m := worktreesModel()
	m.screen = screenAgents
	m.width, m.height = 200, 40
	m.listing.workflows = []panel.Workflow{{ID: "w1", Name: "weave"}, {ID: "w2", Name: "gadgets"}}
	m.listing.agents = []agentItem{
		{agent: panel.Agent{
			ID:             "delta",
			Name:           "delta",
			WorkflowID:     "w1",
			CreatedAt:      at(30),
			StateChangedAt: at(10),
		}},
		{agent: panel.Agent{
			ID:             "charlie",
			Name:           "charlie",
			WorkflowID:     "w1",
			ParentAgentID:  &alpha,
			CreatedAt:      at(25),
			StateChangedAt: at(5),
		}},
		{agent: panel.Agent{
			ID:             "zulu",
			Name:           "Zulu",
			WorkflowID:     "w1",
			CreatedAt:      at(20),
			StateChangedAt: at(30),
		}},
		{agent: panel.Agent{
			ID:             "alpha",
			Name:           "alpha",
			WorkflowID:     "w1",
			CreatedAt:      at(10),
			StateChangedAt: at(20),
		}},
		{agent: panel.Agent{
			ID:             "bob",
			Name:           "bob",
			WorkflowID:     "w1",
			ParentAgentID:  &alpha,
			CreatedAt:      at(5),
			StateChangedAt: at(40),
		}},
		{agent: panel.Agent{
			ID:             "echo",
			Name:           "echo",
			WorkflowID:     "w2",
			CreatedAt:      at(0),
			StateChangedAt: at(50),
		}},
	}

	return m
}

// TestAgentRowsOrderFamiliesByTheColumn covers the families under a workflow ordered by their
// head in the chosen column and direction, each family's children following their head in the
// same order, created down being the listing's own order, a name in capitals sorting as its
// lowercase, and the workflows keeping theirs.
func TestAgentRowsOrderFamiliesByTheColumn(t *testing.T) {
	tests := []struct {
		name  string
		state tabsState
		want  []string
	}{
		{
			name:  "created down is the fallback and the listing's own order",
			state: tabsState{},
			want:  []string{"workflow:w1", "agent:delta", "agent:zulu", "agent:alpha", "agent:charlie", "agent:bob", "workflow:w2", "agent:echo"},
		},
		{
			name:  "created up is the oldest family first",
			state: tabsState{chosen: "created", direction: tabUp},
			want:  []string{"workflow:w1", "agent:alpha", "agent:bob", "agent:charlie", "agent:zulu", "agent:delta", "workflow:w2", "agent:echo"},
		},
		{
			name:  "activity down is the family whose head changed state last first",
			state: tabsState{chosen: "activity", direction: tabDown},
			want:  []string{"workflow:w1", "agent:zulu", "agent:alpha", "agent:bob", "agent:charlie", "agent:delta", "workflow:w2", "agent:echo"},
		},
		{
			name:  "name up",
			state: tabsState{chosen: "name", direction: tabUp},
			want:  []string{"workflow:w1", "agent:alpha", "agent:bob", "agent:charlie", "agent:delta", "agent:zulu", "workflow:w2", "agent:echo"},
		},
		{
			name:  "name down",
			state: tabsState{chosen: "name", direction: tabDown},
			want:  []string{"workflow:w1", "agent:zulu", "agent:delta", "agent:alpha", "agent:charlie", "agent:bob", "workflow:w2", "agent:echo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := agentsSortModel()
			m.agents.sort = tt.state

			assert.Equal(t, tt.want, xslice.Map(m.agentRows(), agentRow.key))
		})
	}
}

// TestAgentRowsFlatOrderTheFamiliesAcrossWorkflows covers the headers going and the families
// walked in the order the sort gave their heads across every workflow, each child still under
// its head with its child flag, and every row still naming its workflow.
func TestAgentRowsFlatOrderTheFamiliesAcrossWorkflows(t *testing.T) {
	tests := []struct {
		name  string
		state tabsState
		want  []string
	}{
		{
			name:  "created down",
			state: tabsState{flat: true},
			want:  []string{"agent:delta", "agent:zulu", "agent:alpha", "agent:charlie", "agent:bob", "agent:echo"},
		},
		{
			name:  "activity down puts the other workflow's agent first",
			state: tabsState{chosen: "activity", flat: true},
			want:  []string{"agent:echo", "agent:zulu", "agent:alpha", "agent:bob", "agent:charlie", "agent:delta"},
		},
		{
			name:  "name up",
			state: tabsState{chosen: "name", direction: tabUp, flat: true},
			want:  []string{"agent:alpha", "agent:bob", "agent:charlie", "agent:delta", "agent:echo", "agent:zulu"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := agentsSortModel()
			m.agents.sort = tt.state

			rows := m.agentRows()
			assert.Equal(t, tt.want, xslice.Map(rows, agentRow.key))
			for _, row := range rows {
				assert.False(t, row.header)
				assert.Equal(t, row.item.agent.WorkflowID, row.workflow.ID)
				assert.Equal(t, row.item.agent.ParentAgentID != nil, row.child)
			}
		})
	}
}

// TestFlatAgentsCursorKeepsItsRowAcrossTheFlip covers the cursor staying on its agent as the
// headers go and come back, and the new agent key still opening under the hovered row's
// workflow while there is no header to hover.
func TestFlatAgentsCursorKeepsItsRowAcrossTheFlip(t *testing.T) {
	m := agentsSortModel()
	m.agents.cursor = 7
	require.Equal(t, "agent:echo", m.agents.list().hovered(m.shared()))

	flat := send(t, send(t, send(t, m, "f"), "shift+tab"), "j")
	require.Equal(t, tabsState{focused: true, onGroup: true, flat: true}, flat.agents.sort, "the created order is untouched by the walk")
	assert.Equal(t, "agent:echo", flat.agents.list().hovered(flat.shared()))
	assert.Equal(t, 5, flat.agents.cursor)
	assert.False(t, flat.agents.isHoveringWorkflow(flat.shared()))
	assert.Equal(t, "w2", flat.agents.enclosingWorkflow(flat.shared()).ID)

	grouped := send(t, flat, "k")
	assert.Equal(t, "agent:echo", grouped.agents.list().hovered(grouped.shared()))
	assert.Equal(t, 7, grouped.agents.cursor)
}

// TestAgentsTabsKeepTheCursorOnItsRow covers the cursor following its row as the tree is
// reordered under it, the walk to a tab reordering nothing until a direction key chooses it,
// and escape leaving the order standing with the cursor still on its row.
func TestAgentsTabsKeepTheCursorOnItsRow(t *testing.T) {
	m := agentsSortModel()
	m.agents.cursor = 3
	require.Equal(t, "agent:alpha", m.agents.list().hovered(m.shared()))

	walked := send(t, send(t, m, "f"), "tab")
	assert.Equal(t, tabsState{focused: true, focus: "activity"}, walked.agents.sort)
	assert.Equal(t, 3, walked.agents.cursor)

	tabbed := send(t, walked, "j")
	assert.Equal(t, tabsState{chosen: "activity", focused: true}, tabbed.agents.sort)
	assert.Equal(t, "agent:alpha", tabbed.agents.list().hovered(tabbed.shared()))
	assert.Equal(t, 2, tabbed.agents.cursor)

	named := send(t, send(t, tabbed, "tab"), "k")
	assert.Equal(t, tabsState{chosen: "name", direction: tabUp, focused: true}, named.agents.sort)
	assert.Equal(t, "agent:alpha", named.agents.list().hovered(named.shared()))
	assert.Equal(t, 1, named.agents.cursor)

	left := send(t, named, "esc")
	assert.Equal(t, tabsState{chosen: "name", direction: tabUp}, left.agents.sort)
	assert.Equal(t, "agent:alpha", left.agents.list().hovered(left.shared()))
	assert.Equal(t, 1, left.agents.cursor)
}

// agentRows is the tree as the agents screen would draw it.
func (m model) agentRows() []agentRow {
	return m.agents.rows(m.shared())
}

// agentHelp is the agents legend as the frame draws it.
func (m model) agentHelp() keyMap {
	return m.listHelp(m.agents.help(m.shared()))
}

func (m model) agentsView() string {
	return m.agents.view(m.shared())
}

func (m model) agentDoc() string {
	return m.agents.doc(m.shared())
}

func (m model) hasNoAgentPane() bool {
	return m.agents.hasNoAgentPane(m.shared())
}

func (m model) selectedAgent() (agentItem, bool) {
	return m.agents.selectedAgent(m.shared())
}

func (m model) hoveredWorkflow() (panel.Workflow, bool) {
	return m.agents.hoveredWorkflow(m.shared())
}

// placeTreeCursor is the agents screen taking the listing as it stands.
func (m *model) placeTreeCursor(selected string) {
	m.agents.apply(m.shared(), selected)
}

// agentsHeader is the list's header as the frame draws it.
func (m model) agentsHeader() string {
	return m.listChromeOf(m.agents.listParams(m.shared())).header(m)
}

func (m model) agentsListView() string {
	return m.screenListView(m.agents.listParams(m.shared()))
}

func (m model) agentsDetails() details {
	return m.agents.details(m.shared())
}

func (m model) deferStaging() (model, tea.Cmd) {
	cmd := m.waitToStage()

	return m, cmd
}

func (m model) readMark(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd := m.agents.readMark(&m, m.shared(), key)

	return m, cmd
}

func (m model) openAgent() (tea.Model, tea.Cmd) {
	cmd := m.agents.openAgent(&m, m.shared())

	return m, cmd
}

func (m model) toggleHiddenAgents() (tea.Model, tea.Cmd) {
	cmd := m.agents.toggleHidden(&m, m.shared(), "")

	return m, cmd
}

func (m model) enterArchive() (tea.Model, tea.Cmd) {
	cmd := m.enter(screenArchive, screenTarget{})

	return m, cmd
}

func (m model) enterSearch() (tea.Model, tea.Cmd) {
	cmd := m.enter(screenSearch, screenTarget{})

	return m, cmd
}

func (m model) openWorktrees(agentID string) model {
	m.enter(screenWorktrees, screenTarget{agentID: agentID})

	return m
}

// The forms the agents screen opens, each reached through the root as the tests here open them.

func (m model) openAgentForm(workflow panel.Workflow, values *agentFormValues) model {
	openAgentForm(&m, workflow, values)

	return m
}

func (m model) openNewChild(parent panel.Agent) (tea.Model, tea.Cmd) {
	cmd := openNewChild(&m, parent)

	return m, cmd
}

func (m model) openChildForm(parent panel.Agent, values *agentFormValues) (tea.Model, tea.Cmd) {
	cmd := openChildForm(&m, parent, values)

	return m, cmd
}

func (m model) openRename(agent panel.Agent) model {
	openRename(&m, agent)

	return m
}

func (m model) openRenameForm(agent panel.Agent, name *string) model {
	openRenameForm(&m, agent, name)

	return m
}

func (m model) openAdoptForm(workflow panel.Workflow, values *adoptFormValues) (tea.Model, tea.Cmd) {
	cmd := openAdoptForm(&m, workflow, values)

	return m, cmd
}

func (m model) archiveAgentsConfirm(agents []panel.Agent) (tea.Model, tea.Cmd) {
	cmd := confirmArchiveAgents(&m, m.listing.liveAgents(), agents)

	return m, cmd
}

func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	cmd := newWorkflowForm(&m)

	return m, cmd
}

func (m model) editWorkflowForm(workflow panel.Workflow) (tea.Model, tea.Cmd) {
	cmd := editWorkflowForm(&m, workflow)

	return m, cmd
}

func (m model) openWorkflowRename(workflow panel.Workflow, values *workflowFormValues) model {
	openWorkflowRename(&m, workflow, values)

	return m
}

func (m model) openWorkflowConfigForm(values *workflowFormValues, target *panel.Workflow) model {
	openWorkflowConfigForm(&m, values, target)

	return m
}

func (m model) archiveWorkflowConfirm(workflow panel.Workflow) model {
	archiveWorkflowConfirm(&m, workflow)

	return m
}

package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingOrchestrators answers the start call and keeps the workflow the panel named.
type recordingOrchestrators struct {
	agentv1connect.AgentServiceClient
	started []string
	err     error
}

// creatingWorkflows answers the workflow create with an id of its own.
type creatingWorkflows struct {
	emptyLists
	id string
}

func (c *creatingWorkflows) CreateWorkflow(
	context.Context,
	*connect.Request[workflowv1.CreateWorkflowRequest],
) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{
		Workflow: &workflowv1.Workflow{Id: c.id},
	}), nil
}

func (r *recordingOrchestrators) StartOrchestrator(
	_ context.Context,
	req *connect.Request[agentv1.StartOrchestratorRequest],
) (*connect.Response[agentv1.StartOrchestratorResponse], error) {
	r.started = append(r.started, req.Msg.GetWorkflowId())
	if r.err != nil {
		return nil, r.err
	}

	return connect.NewResponse(&agentv1.StartOrchestratorResponse{}), nil
}

func (r *recordingOrchestrators) ListAgents(
	context.Context,
	*connect.Request[agentv1.ListAgentsRequest],
) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// orchestratorModel is the agents screen standing on a workflow row, holding whatever agents
// the test names.
func orchestratorModel(agents agentv1connect.AgentServiceClient, items ...agentItem) model {
	return model{
		api:    api{agents: agents, workflows: emptyLists{}},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		width:  120,
		height: 40,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			agents:    items,
		},
	}
}

// orchestratorItem is a workflow's orchestrator, alive and thinking.
func orchestratorItem() agentItem {
	return agentItem{agent: panel.Agent{
		ID:         "o1",
		Name:       "orchestrator",
		Pane:       "%9",
		Harness:    "claude",
		State:      panel.StateThinking,
		WorkflowID: "w1",
		Role:       panel.RoleOrchestrator,
	}, alive: true}
}

// failedOrchestratorDoc stands for the placeholder a failed agent draws, which the table cannot
// build without a model.
const failedOrchestratorDoc = "<failed>"

// dormantOrchestrator is one whose CLI has exited, which leaves the row live and the pane gone.
func dormantOrchestrator() agentItem {
	it := orchestratorItem()
	it.alive = false
	it.agent.State = panel.StateIdle

	return it
}

// failedOrchestrator is one weave could not start.
func failedOrchestrator() agentItem {
	it := orchestratorItem()
	it.alive = false
	it.agent.State = panel.StateFailedToCreate

	return it
}

// TestOrchestratorIsNotARowOfItsOwn pins that the agent standing behind a workflow is drawn on
// the workflow's row rather than under it, and that no key reveals it.
func TestOrchestratorIsNotARowOfItsOwn(t *testing.T) {
	tests := []struct {
		name       string
		showHidden bool
	}{
		{name: "with hidden rows kept back"},
		{name: "with hidden rows shown", showHidden: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			worker := agentItem{agent: panel.Agent{
				ID: "a1", Name: "telescope", State: panel.StateIdle, WorkflowID: "w1",
			}, alive: true}

			m := orchestratorModel(&recordingOrchestrators{}, orchestratorItem(), worker)
			m.tree.showHidden = tt.showHidden

			rows := m.agentRows()
			require.Len(t, rows, 2, "the workflow header and the one agent under it")

			assert.True(t, rows[0].header)
			assert.Equal(t, "a1", rows[1].item.agent.ID)
			assert.Zero(t, rows[0].hiddenCount, "it is not counted as hidden either")

			require.NotNil(t, rows[0].orchestrator)
			assert.Equal(t, "o1", rows[0].orchestrator.agent.ID)
		})
	}
}

// TestWorkflowRowShowsItsOrchestrator covers what the workflow row draws and stages once an
// orchestrator stands behind it.
func TestWorkflowRowShowsItsOrchestrator(t *testing.T) {
	tests := []struct {
		name        string
		items       []agentItem
		wantPane    bool
		wantStateOf panel.State
		// wantDoc is the markdown the stage draws when there is no pane.
		wantDoc string
	}{
		{
			name:  "a workflow that has never had one",
			items: nil,
		},
		{
			name:        "a workflow whose orchestrator is running",
			items:       []agentItem{orchestratorItem()},
			wantPane:    true,
			wantStateOf: panel.StateThinking,
		},
		{
			name:        "a workflow whose orchestrator has stopped",
			items:       []agentItem{dormantOrchestrator()},
			wantStateOf: panel.StateIdle,
			wantDoc:     dormantPlaceholder,
		},
		{
			name:        "a workflow whose orchestrator failed to start",
			items:       []agentItem{failedOrchestrator()},
			wantStateOf: panel.StateFailedToCreate,
			wantDoc:     failedOrchestratorDoc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := orchestratorModel(&recordingOrchestrators{}, tt.items...)
			m.tree.cursor = 0

			wantDoc := tt.wantDoc
			if wantDoc == failedOrchestratorDoc {
				wantDoc = m.failedPlaceholder(failedOrchestrator().agent)
			}

			rows := m.agentRows()
			require.True(t, rows[0].header)

			assert.Equal(t, !tt.wantPane, m.hasNoAgentPane(),
				"the window is left to the panel only when there is no pane to show")

			if len(tt.items) == 0 {
				assert.Equal(t, workflowContextMarkdown(rows[0].workflow), m.agentDoc(),
					"a workflow with no orchestrator still draws its shared context")

				return
			}

			if tt.wantPane {
				return
			}

			assert.Equal(t, wantDoc, m.agentDoc(),
				"the stage says why there is no conversation to show")
		})
	}
}

// TestTalkingToAWorkflowStartsItsOrchestrator pins that open answers a workflow row the way it
// answers an agent row, starting what is not running.
func TestTalkingToAWorkflowStartsItsOrchestrator(t *testing.T) {
	agents := &recordingOrchestrators{}

	m := orchestratorModel(agents)
	m.tree.cursor = 0

	pressed, cmd := m.Update(keyMsg("enter"))
	written, ok := pressed.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	assert.Equal(t, []string{"w1"}, agents.started)
}

// TestFocusAloneStartsNothing is the other half of that key: moving the cursor onto a workflow
// is a read, and must not start an agent.
func TestFocusAloneStartsNothing(t *testing.T) {
	agents := &recordingOrchestrators{}

	m := orchestratorModel(agents)
	m.tree.cursor = 1

	// A cursor move driven the same way the key is, so a start hiding behind focus would be
	// collected and sent rather than dropped.
	moved, cmd := m.Update(keyMsg("k"))
	written, ok := moved.(model)
	require.True(t, ok)

	settle(t, written, cmd)

	assert.Zero(t, written.tree.cursor, "the cursor did land on the workflow row")
	assert.Empty(t, agents.started)
}

// TestASearchHitLandsOnTheWorkflowRow pins where a hit on a folded orchestrator goes.
func TestASearchHitLandsOnTheWorkflowRow(t *testing.T) {
	worker := agentItem{agent: panel.Agent{
		ID: "a1", Name: "telescope", State: panel.StateIdle, WorkflowID: "w1",
	}, alive: true}

	m := orchestratorModel(&recordingOrchestrators{}, worker, orchestratorItem())
	m.tree.cursor = 1

	m.revealAgent("o1")

	rows := m.agentRows()
	require.True(t, rows[m.tree.cursor].header, "the cursor lands on the workflow it stands behind")
	assert.Equal(t, "w1", rows[m.tree.cursor].workflow.ID)
}

// TestArchiveDrawsAnOrchestratorOfItsOwn pins the one screen that must show one.
func TestArchiveDrawsAnOrchestratorOfItsOwn(t *testing.T) {
	rows := treeRows(treeRowsParams{
		workflows:  []panel.Workflow{{ID: "w1", Name: "backend"}},
		agents:     []agentItem{orchestratorItem()},
		showHidden: true,
	})

	require.Len(t, rows, 2)
	assert.True(t, rows[0].header)
	assert.Nil(t, rows[0].orchestrator, "the archive folds nothing onto the header")
	assert.Equal(t, "o1", rows[1].item.agent.ID)
}

// TestTheWorkflowRowNamesItsOrchestratorsPane pins the pane the warm-up clock and the re-stage
// both read, which a header row used to answer with nothing.
func TestTheWorkflowRowNamesItsOrchestratorsPane(t *testing.T) {
	m := orchestratorModel(&recordingOrchestrators{}, orchestratorItem())
	m.tree.cursor = 0

	pane, harness := m.selectedAgentPane()
	assert.Equal(t, "%9", pane)
	assert.Equal(t, "claude", harness)
}

// TestCreatingAWorkflowStartsItsOrchestrator pins that a workflow made in the panel comes with the
// agent that stands behind it.
func TestCreatingAWorkflowStartsItsOrchestrator(t *testing.T) {
	agents := &recordingOrchestrators{}

	m := orchestratorModel(agents)
	m.api.workflows = &creatingWorkflows{id: "w2"}

	settle(t, m, m.createWorkflowCmd(panel.WorkflowConfig{Name: "backend"}))

	assert.Equal(t, []string{"w2"}, agents.started)
}

// TestCreatingAWorkflowStandsWhenItsOrchestratorWillNot pins that a refused orchestrator leaves the
// workflow standing.
func TestCreatingAWorkflowStandsWhenItsOrchestratorWillNot(t *testing.T) {
	agents := &recordingOrchestrators{err: errors.New("the person refused the directory")}

	m := orchestratorModel(agents)
	m.api.workflows = &creatingWorkflows{id: "w2"}

	var result writeResult
	for _, msg := range produced(m.createWorkflowCmd(panel.WorkflowConfig{Name: "backend"})) {
		if written, ok := msg.(writeResult); ok {
			result = written
		}
	}

	require.NoError(t, result.err, "the workflow was created, so the write did not fail")
	require.NotNil(t, result.written)
	assert.Equal(t, workflowRowKey("w2"), result.written.row)
	assert.Empty(t, result.written.pane, "and no pane is staged for an orchestrator that never started")
}

// TestAnOrchestratorsChildStandsOnItsOwn pins that an agent whose parent is folded onto the
// workflow row is drawn rather than left with no row anywhere.
func TestAnOrchestratorsChildStandsOnItsOwn(t *testing.T) {
	child := agentItem{agent: panel.Agent{
		ID: "a1", Name: "reader", State: panel.StateIdle, WorkflowID: "w1",
		ParentAgentID: xptr.Ptr("o1"),
	}, alive: true}

	m := orchestratorModel(&recordingOrchestrators{}, orchestratorItem(), child)

	rows := m.agentRows()
	require.Len(t, rows, 2, "the workflow header and the child standing under it")
	assert.Equal(t, "a1", rows[1].item.agent.ID)
	assert.False(t, rows[1].child, "it hangs off no row above it, since its parent draws none")
	assert.Equal(t, 1, m.agentCount("w1"), "and the header counts the row it drew")
}

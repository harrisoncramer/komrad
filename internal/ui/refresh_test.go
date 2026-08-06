package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settle delivers what a write asked for back into the model, the way the
// bubbletea runtime does with the command an update hands back. Every write the
// panel makes runs as a command, so a case that only pressed the key would be
// asserting against the keypress rather than against what came of it. Messages that
// belong to a form rather than to the panel's own loop are left alone.
func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range produced(cmd) {
		switch msg.(type) {
		case writeResult, refreshResult, actionsResult:
		default:
			continue
		}

		updated, _ := m.Update(msg)
		next, ok := updated.(model)
		require.True(t, ok)
		m = next
	}

	return m
}

// produced runs a command and flattens what came out of it, since a batch is a
// command that hands back further commands rather than a message.
func produced(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, batched := range msg {
			msgs = append(msgs, produced(batched)...)
		}

		return msgs
	default:
		return []tea.Msg{msg}
	}
}

// TestRefreshAppliesWhatAnswered covers a pass where one listing failed. They all
// go to the same server, so a failure is reported as the server not answering, but
// the rows that did come back are still taken: throwing away a whole pass over one
// listing is how a slow call ends up costing the lists it has nothing to do with.
func TestRefreshAppliesWhatAnswered(t *testing.T) {
	m := model{
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree:   treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}},
	}

	m.applyRefresh(refreshResult{
		agents:       []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
		workflowsErr: errors.New("connection reset"),
	})

	assert.Len(t, m.tree.agents, 1, "the listing that answered is taken")
	require.Len(t, m.tree.workflows, 1, "the one that did not keeps the rows it had")
	assert.Equal(t, "api", m.tree.workflows[0].Name)
	assert.Error(t, m.unreachable, "and the panel says the server is not answering")
}

// TestStaleRefreshIsDropped covers the race the panel used to lose. A pass reads
// the store a moment before a keypress writes to it and lands a moment after, so
// taking it would put the hidden row, the deleted row, or the old mark back and read
// as a keypress that did nothing.
func TestStaleRefreshIsDropped(t *testing.T) {
	m := model{
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree:   treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}},
	}

	inFlight := refreshResult{
		epoch:  m.epoch,
		agents: []agentItem{{agent: agentstore.Agent{Name: "hidden-since", WorkflowID: "w1"}}},
	}
	m.beginWrite()

	m.applyRefresh(inFlight)

	assert.Empty(t, m.tree.agents, "a pass from before the write is not what the panel shows after it")
}

// TestWriteStandsTheRefreshDown covers the other half of that race. A pass started
// while a write is on its way to the store would read the rows it is about to
// replace, so the tick skips it and the write asks for one as it lands.
func TestWriteStandsTheRefreshDown(t *testing.T) {
	m := model{api: api{agents: emptyLists{}, workflows: emptyLists{}, schedules: emptyLists{}}}

	require.NotNil(t, m.refreshCmd(), "an idle panel lists on the tick")

	write := m.writeCmd("Could not write", func(api) error { return nil })
	assert.Nil(t, m.refreshCmd(), "one with a write in flight waits for it")

	settled := settle(t, m, write)
	assert.NotNil(t, settled.refreshCmd(), "and lists again once it has landed")
}

// TestFirstListingLandsOnTheFirstAgent covers where the cursor starts. Nothing is
// listed when the panel paints its first frame, so the cursor is placed by the first
// pass rather than at startup, and a cursor left where a never-listed one sits would
// open on the workflow header above the agents.
func TestFirstListingLandsOnTheFirstAgent(t *testing.T) {
	m := model{
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree:   treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}},
	}

	m.applyRefresh(refreshResult{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
		agents:    []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
	})

	selected, ok := m.selectedAgent()
	require.True(t, ok, "the cursor is on an agent rather than on the folder above it")
	assert.Equal(t, "api-refactor", selected.agent.Name)
}

// TestCreatedAgentReceivesFocus covers the write result carrying the created agent back
// to the panel. Its row does not exist when the form closes, so the id has to survive until
// the listing that includes it arrives.
func TestCreatedAgentReceivesFocus(t *testing.T) {
	agents := &formRecorder{}
	tmux := &recordingTmux{}
	m := model{
		api:    api{agents: agents, actions: agents},
		tmux:   tmux,
		stage:  NewStage(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
			agents: []agentItem{{agent: agentstore.Agent{
				ID: "old", Name: "api-refactor", WorkflowID: "w1",
			}}},
			rowCursor: rowCursor{cursor: 1},
			placed:    true,
		},
	}

	workflow := m.tree.workflows[0]
	opened, _ := m.openNewAgent(&workflow)
	m = asModel(t, opened)
	m.values.agent.Title = "api-tests"
	m.values.agent.Dir = "/work/api"
	submitted, cmd := m.submitAgent()
	m = asModel(t, submitted)

	for _, msg := range produced(cmd) {
		if result, ok := msg.(writeResult); ok {
			updated, _ := m.applyWrite(result)
			m = asModel(t, updated)
		}
	}

	m.applyRefresh(refreshResult{
		epoch:     m.epoch,
		workflows: m.tree.workflows,
		agents: []agentItem{
			{agent: agentstore.Agent{ID: "old", Name: "api-refactor", WorkflowID: "w1"}},
			{agent: agentstore.Agent{ID: "new", Name: "api-tests", WorkflowID: "w1"}},
		},
	})

	selected, ok := m.selectedAgent()
	require.True(t, ok)
	assert.Equal(t, "new", selected.agent.ID)
}

// TestCreatedWorkflowReceivesFocus covers the same for a folder. A workflow made from the
// panel is the one the user is about to fill, so the cursor lands on its header rather than
// staying on whatever it was hovering when the form opened.
func TestCreatedWorkflowReceivesFocus(t *testing.T) {
	recorder := &formRecorder{}
	m := model{
		api:    api{agents: recorder, workflows: recorder, actions: recorder},
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
			placed:    true,
		},
	}

	opened, _ := m.newWorkflowForm()
	m = asModel(t, opened)
	m.values.workflow.Name = "web"
	submitted, cmd := m.submitWorkflowForm()
	m = asModel(t, submitted)

	for _, msg := range produced(cmd) {
		if result, ok := msg.(writeResult); ok {
			updated, _ := m.applyWrite(result)
			m = asModel(t, updated)
		}
	}

	m.applyRefresh(refreshResult{
		epoch: m.epoch,
		workflows: []workflowstore.Workflow{
			{ID: "w1", Name: "api"},
			{ID: "new", Name: "web"},
		},
	})

	hovered, ok := m.hoveredWorkflow()
	require.True(t, ok, "the cursor is on a folder header")
	assert.Equal(t, "new", hovered.ID)
}

// TestCursorHoldsItsFolderAcrossARefresh covers a folder archived from somewhere else
// while the cursor sits on a folder below it. A header is a row like any other, so the
// cursor has to follow the folder it names rather than the index it was drawn at.
func TestCursorHoldsItsFolderAcrossARefresh(t *testing.T) {
	m := model{
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}},
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "a1", Name: "api-refactor", WorkflowID: "w1"}},
				{agent: agentstore.Agent{ID: "a2", Name: "infra-audit", WorkflowID: "w2"}},
			},
			rowCursor: rowCursor{cursor: 2},
			placed:    true,
		},
	}

	m.applyRefresh(refreshResult{
		workflows: []workflowstore.Workflow{{ID: "w2", Name: "infra"}},
		agents:    []agentItem{{agent: agentstore.Agent{ID: "a2", Name: "infra-audit", WorkflowID: "w2"}}},
	})

	hovered, ok := m.hoveredWorkflow()
	require.True(t, ok, "the cursor is still on a folder rather than on the agent under it")
	assert.Equal(t, "infra", hovered.Name)
}

// TestCursorHoldsItsAgentWhenAFolderArrives covers the other direction, a folder created
// from somewhere else sorting in above the cursor. The folders land before the agents, so
// a cursor read after they have is read against a tree that is half of each pass.
func TestCursorHoldsItsAgentWhenAFolderArrives(t *testing.T) {
	m := model{
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w2", Name: "infra"}},
			agents: []agentItem{
				{agent: agentstore.Agent{ID: "b1", Name: "infra-audit", WorkflowID: "w2"}},
				{agent: agentstore.Agent{ID: "b2", Name: "infra-upgrade", WorkflowID: "w2"}},
			},
			rowCursor: rowCursor{cursor: 2},
			placed:    true,
		},
	}

	m.applyRefresh(refreshResult{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}},
		agents: []agentItem{
			{agent: agentstore.Agent{ID: "b1", Name: "infra-audit", WorkflowID: "w2"}},
			{agent: agentstore.Agent{ID: "b2", Name: "infra-upgrade", WorkflowID: "w2"}},
		},
	})

	selected, ok := m.selectedAgent()
	require.True(t, ok, "the cursor is on an agent")
	assert.Equal(t, "infra-upgrade", selected.agent.Name)
}

// TestArchiveCursorHoldsItsAgentWhenAFolderGoes covers the same shape on the archive. Its
// folders are partly the live ones, which a pass replaces before the archive's own listings
// land, so the row under the cursor has to be read before any of them do.
func TestArchiveCursorHoldsItsAgentWhenAFolderGoes(t *testing.T) {
	m := model{
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenArchive,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}},
		},
		archive: archiveState{
			agents: []agentstore.Agent{
				{ID: "x1", Name: "api-spike", WorkflowID: "w1"},
				{ID: "x2", Name: "infra-audit", WorkflowID: "w2"},
				{ID: "x3", Name: "infra-upgrade", WorkflowID: "w2"},
			},
			rowCursor: rowCursor{cursor: 3},
		},
	}

	m.applyRefresh(refreshResult{
		workflows:      []workflowstore.Workflow{{ID: "w2", Name: "infra"}},
		archivedListed: true,
		archived: []agentstore.Agent{
			{ID: "x2", Name: "infra-audit", WorkflowID: "w2"},
			{ID: "x3", Name: "infra-upgrade", WorkflowID: "w2"},
		},
	})

	selected, ok := m.selectedArchived()
	require.True(t, ok, "the cursor is on an archived agent")
	assert.Equal(t, "infra-audit", selected.Name)
}

// emptyLists answers every listing a pass makes with nothing, so a case that settles
// one does not have to stub the listings it is not about. The embedded interfaces are
// nil, so a method nothing here answers panics rather than quietly returning a zero
// value.
type emptyLists struct {
	agentv1connect.AgentServiceClient
	workflowv1connect.WorkflowServiceClient
	schedulev1connect.ScheduleServiceClient
}

func (emptyLists) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

func (emptyLists) ListWorkflows(context.Context, *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	return connect.NewResponse(&workflowv1.ListWorkflowsResponse{}), nil
}

func (emptyLists) ListSchedules(context.Context, *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	return connect.NewResponse(&schedulev1.ListSchedulesResponse{}), nil
}

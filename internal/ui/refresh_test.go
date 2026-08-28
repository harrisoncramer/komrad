package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
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

func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range produced(cmd) {
		switch msg.(type) {
		case writeResult, refreshResult:
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

// TestRefreshAppliesWhatAnswered covers a pass where one listing failed, which still takes
// the rows that did come back rather than throwing the whole pass away.
func TestRefreshAppliesWhatAnswered(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}},
	}

	m.applyRefresh(refreshResult{
		agents:       []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
		workflowsErr: errors.New("connection reset"),
	})

	assert.Len(t, m.listing.agents, 1, "the listing that answered is taken")
	require.Len(t, m.listing.workflows, 1, "the one that did not keeps the rows it had")
	assert.Equal(t, "api", m.listing.workflows[0].Name)
	assert.Error(t, m.unreachable, "and the panel says the server is not answering")
}

// TestStaleRefreshIsDropped covers a pass that read the store just before a keypress wrote
// to it and landed just after, which would put the old row back.
func TestStaleRefreshIsDropped(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}},
	}

	inFlight := refreshResult{
		epoch:  m.epoch,
		agents: []agentItem{{agent: agentstore.Agent{Name: "hidden-since", WorkflowID: "w1"}}},
	}
	m.beginWrite()

	m.applyRefresh(inFlight)

	assert.Empty(t, m.listing.agents, "a pass from before the write is not what the panel shows after it")
}

// TestWriteStandsTheRefreshDown covers the other half of that race, where the tick skips a
// pass while a write is on its way and the write asks for one as it lands.
func TestWriteStandsTheRefreshDown(t *testing.T) {
	m := model{api: api{agents: emptyLists{}, workflows: emptyLists{}, schedules: emptyLists{}}}

	require.NotNil(t, m.refreshCmd(), "an idle panel lists on the tick")

	write := m.writeCmd("Could not write", func(api) error { return nil })
	assert.Nil(t, m.refreshCmd(), "one with a write in flight waits for it")

	settled := settle(t, m, write)
	assert.NotNil(t, settled.refreshCmd(), "and lists again once it has landed")
}

// TestFirstListingLandsOnTheFirstAgent covers the cursor being placed by the first pass,
// since one placed at startup would open on the workflow header above the agents.
func TestFirstListingLandsOnTheFirstAgent(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}},
	}

	m.applyRefresh(refreshResult{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
		agents:    []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
	})

	selected, ok := m.selectedAgent()
	require.True(t, ok, "the cursor is on an agent rather than on the folder above it")
	assert.Equal(t, "api-refactor", selected.agent.Name)
}

// TestCreatedAgentReceivesFocus covers the id of a created agent surviving until the listing
// that holds its row arrives, since the row does not exist when the form closes.
func TestCreatedAgentReceivesFocus(t *testing.T) {
	agents := &formRecorder{}
	tmux := &recordingTmux{}
	m := model{
		api:    api{agents: agents, actions: agents},
		tmux:   tmux,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}, agents: []agentItem{{agent: agentstore.Agent{
			ID: "old", Name: "api-refactor", WorkflowID: "w1",
		}}}}, tree: treeState{rowCursor: rowCursor{cursor: 1}, placed: true},
	}

	workflow := m.listing.workflows[0]
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
		workflows: m.listing.workflows,
		agents: []agentItem{
			{agent: agentstore.Agent{ID: "old", Name: "api-refactor", WorkflowID: "w1"}},
			{agent: agentstore.Agent{ID: "new", Name: "api-tests", WorkflowID: "w1"}},
		},
	})

	selected, ok := m.selectedAgent()
	require.True(t, ok)
	assert.Equal(t, "new", selected.agent.ID)
}

// TestUnrelatedWriteDoesNotCloseTheSetupPane covers a rename or an archive landing while a
// create's setup action is still streaming into its pane: that write's own success is not
// the create's, so it must not tear down the pane out from under the setup action.
func TestUnrelatedWriteDoesNotCloseTheSetupPane(t *testing.T) {
	tmux := &recordingTmux{}
	m := model{
		tmux:   tmux,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		info:   infoState{kind: infoSetup},
	}

	updated, _ := m.applyWrite(writeResult{summary: "Could not rename the agent"})
	got := asModel(t, updated)

	assert.Equal(t, infoSetup, got.info.kind, "a write that never touched the setup pane leaves it alone")
}

// TestCreatedWorkflowReceivesFocus covers the same for a folder, where the cursor lands on
// the new header rather than staying where the form opened.
func TestCreatedWorkflowReceivesFocus(t *testing.T) {
	recorder := &formRecorder{}
	m := model{
		api:     api{agents: recorder, workflows: recorder, actions: recorder},
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}}}, tree: treeState{placed: true},
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

// TestCursorHoldsItsFolderAcrossARefresh covers a folder archived from somewhere else, after
// which the cursor follows the folder it names rather than the index it was drawn at.
func TestCursorHoldsItsFolderAcrossARefresh(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "a1", Name: "api-refactor", WorkflowID: "w1"}},
			{agent: agentstore.Agent{ID: "a2", Name: "infra-audit", WorkflowID: "w2"}},
		}}, tree: treeState{rowCursor: rowCursor{cursor: 2}, placed: true},
	}

	m.applyRefresh(refreshResult{
		workflows: []workflowstore.Workflow{{ID: "w2", Name: "infra"}},
		agents:    []agentItem{{agent: agentstore.Agent{ID: "a2", Name: "infra-audit", WorkflowID: "w2"}}},
	})

	hovered, ok := m.hoveredWorkflow()
	require.True(t, ok, "the cursor is still on a folder rather than on the agent under it")
	assert.Equal(t, "infra", hovered.Name)
}

// TestCursorHoldsItsAgentWhenAFolderArrives covers a folder sorting in above the cursor,
// where the folders land before the agents and leave the tree half of each pass.
func TestCursorHoldsItsAgentWhenAFolderArrives(t *testing.T) {
	m := model{
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w2", Name: "infra"}}, agents: []agentItem{
			{agent: agentstore.Agent{ID: "b1", Name: "infra-audit", WorkflowID: "w2"}},
			{agent: agentstore.Agent{ID: "b2", Name: "infra-upgrade", WorkflowID: "w2"}},
		}}, tree: treeState{rowCursor: rowCursor{cursor: 2}, placed: true},
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

// TestArchiveCursorHoldsItsAgentWhenAFolderGoes covers the same shape on the archive, whose
// folders are replaced before its own listings land.
func TestArchiveCursorHoldsItsAgentWhenAFolderGoes(t *testing.T) {
	m := model{
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		screen:  screenArchive,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}, {ID: "w2", Name: "infra"}}},
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

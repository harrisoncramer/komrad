package ui

import (
	"context"
	"errors"
	"testing"

	"claude-agents/internal/appconfig"
	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"
	agentv1 "claude-agents/proto/gen/agent/v1"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	schedulev1 "claude-agents/proto/gen/schedule/v1"
	"claude-agents/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settle delivers what an action asked for back into the model, the way the
// bubbletea runtime does with the command an action hands back. Every write the
// panel makes runs as a command, so a case that only pressed the key would be
// asserting against the keypress rather than against what came of it. Messages that
// belong to a form rather than to the panel's own loop are left alone.
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
		stage:          NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:           appconfig.DefaultKeybindings(),
		screen:         screenAgents,
		workflows:      []workflowstore.Workflow{{ID: "w1", Name: "api"}},
		workflowCursor: 0,
	}

	m.applyRefresh(refreshResult{
		agents:       []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
		workflowsErr: errors.New("connection reset"),
	})

	assert.Len(t, m.agents, 1, "the listing that answered is taken")
	require.Len(t, m.workflows, 1, "the one that did not keeps the rows it had")
	assert.Equal(t, "api", m.workflows[0].Name)
	assert.Error(t, m.unreachable, "and the panel says the server is not answering")
}

// TestStaleRefreshIsDropped covers the race the panel used to lose. A pass reads
// the store a moment before a keypress writes to it and lands a moment after, so
// taking it would put the hidden row, the deleted row, or the old mark back and read
// as a keypress that did nothing.
func TestStaleRefreshIsDropped(t *testing.T) {
	m := model{
		stage:     NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:      appconfig.DefaultKeybindings(),
		screen:    screenAgents,
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
	}

	inFlight := refreshResult{
		epoch:  m.epoch,
		agents: []agentItem{{agent: agentstore.Agent{Name: "hidden-since", WorkflowID: "w1"}}},
	}
	m.beginWrite()

	m.applyRefresh(inFlight)

	assert.Empty(t, m.agents, "a pass from before the write is not what the panel shows after it")
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
		stage:     NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:      appconfig.DefaultKeybindings(),
		screen:    screenAgents,
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
	}

	m.applyRefresh(refreshResult{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "api"}},
		agents:    []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}},
	})

	selected, ok := m.selectedAgent()
	require.True(t, ok, "the cursor is on an agent rather than on the folder above it")
	assert.Equal(t, "api-refactor", selected.agent.Name)
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

package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	"weave/internal/client"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

type emptyLists struct {
	agentv1connect.AgentServiceClient
	workflowv1connect.WorkflowServiceClient
	schedulev1connect.ScheduleServiceClient
	worktreev1connect.WorktreeServiceClient
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

func (emptyLists) ListWorktrees(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[worktreev1.ListWorktreesResponse], error) {
	return connect.NewResponse(&worktreev1.ListWorktreesResponse{}), nil
}

func (emptyLists) GetWorktree(_ context.Context, req *connect.Request[worktreev1.GetWorktreeRequest]) (*connect.Response[worktreev1.GetWorktreeResponse], error) {
	return nil, connect.NewError(connect.CodeNotFound, errors.New(req.Msg.GetDir()+" is not under the worktree directories"))
}

func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range collectMessages(cmd) {
		switch msg.(type) {
		case writeResult, client.WatchBatch:
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

// applyListing replaces the model's workflows and clears its agents.
func applyListing(m *model, workflows []panel.Workflow) {
	m.withListing(func(current *listing) {
		current.agents, current.workflows = nil, workflows
	})
}

func collectMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var msgs []tea.Msg
		for _, batched := range msg {
			msgs = append(msgs, collectMessages(batched)...)
		}

		return msgs
	default:
		return []tea.Msg{msg}
	}
}

func pressLogs(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.Update(keyMsg("l"))
	shown, ok := next.(model)
	require.True(t, ok)

	return shown
}

func newFilterModel() model {
	tmux := &recordingTmux{}
	workflow := panel.Workflow{ID: "w1", Name: "core"}

	return model{
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []panel.Workflow{workflow},
			agents: []panel.AgentRow{
				{Agent: panel.Agent{
					ID:         "a1",
					Name:       "billing",
					WorkflowID: "w1",
				}},
				{Agent: panel.Agent{
					ID:         "a2",
					Name:       "payouts",
					WorkflowID: "w1",
				}},
			},
		},
		agents: agentsScreen{rowCursor: rowCursor{cursor: 1}},
	}
}

package ui

import (
	"context"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeArchiveWorkflows struct {
	workflowv1connect.WorkflowServiceClient
	restored []string
}

type fakeArchiveAgents struct {
	agentv1connect.AgentServiceClient
	restored []string
	deleted  []string
}

func (f *fakeArchiveWorkflows) RestoreWorkflow(_ context.Context, req *connect.Request[workflowv1.RestoreWorkflowRequest]) (*connect.Response[workflowv1.RestoreWorkflowResponse], error) {
	f.restored = append(f.restored, req.Msg.GetId())

	return connect.NewResponse(&workflowv1.RestoreWorkflowResponse{}), nil
}

func (f *fakeArchiveAgents) RestoreAgents(_ context.Context, req *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	f.restored = append(f.restored, req.Msg.GetIds()...)

	response := &agentv1.RestoreAgentsResponse{Agents: xslice.Map(req.Msg.GetIds(), func(id string) *agentv1.Agent {
		return &agentv1.Agent{Id: id, Name: "api-refactor"}
	})}

	return connect.NewResponse(response), nil
}

func (f *fakeArchiveAgents) DeleteAgents(_ context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	f.deleted = append(f.deleted, req.Msg.GetIds()...)

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func archiveModel(agents agentv1connect.AgentServiceClient, workflows workflowv1connect.WorkflowServiceClient, keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api: api{
			agents:    agents,
			workflows: workflows,
			schedules: emptyLists{},
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenArchive,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "backend"}},
			archivedAgents: archivedItems([]panel.Agent{{
				ID:         "a1",
				Name:       "api-refactor",
				WorkflowID: "w1",
				State:      panel.StateIdle,
			}}),
		},
		archive: archiveScreen{rowCursor: rowCursor{cursor: 1}},
	}
}

func archivedWorkflow(id, name string) panel.Workflow {
	at := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)

	return panel.Workflow{
		ID:         id,
		Name:       name,
		ArchivedAt: &at,
	}
}

func splitArchiveModel(t *testing.T) model {
	t.Helper()

	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.width, m.height = 140, 24
	m.frame.Attach("%0")
	m.frame.Solo(true)

	return m
}

func archivedItems(agents []panel.Agent) []panel.AgentRow {
	return xslice.Map(agents, func(agent panel.Agent) panel.AgentRow {
		return panel.AgentRow{Agent: agent}
	})
}

func (m model) archiveHelp() keyMap {
	return m.listHelpOf(m.archive.listParams(m.shared()))
}

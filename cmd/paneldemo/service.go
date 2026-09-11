package main

import (
	"context"

	"weave/cmd/paneldemo/scenario"
	agentv1 "weave/proto/gen/agent/v1"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// agentService answers the panel's reads about agents out of the scenario, with the review half
// embedded so the review screen finds the calls it makes.
type agentService struct {
	*reviewFake
	held scenario.Scenario
}

func (s agentService) ListAgents(
	_ context.Context,
	req *connect.Request[agentv1.ListAgentsRequest],
) (*connect.Response[agentv1.ListAgentsResponse], error) {
	live, archived := s.held.Agents, s.held.ArchivedAgents
	if req.Msg.GetArchived() {
		live = archived
	}

	return connect.NewResponse(&agentv1.ListAgentsResponse{Agents: live}), nil
}

func (s agentService) ListMail(
	_ context.Context,
	req *connect.Request[agentv1.ListMailRequest],
) (*connect.Response[agentv1.ListMailResponse], error) {
	held := req.Msg.GetId()

	var found []*agentv1.Mail

	for _, mail := range s.held.Mail {
		sent := mail.GetFromAgentId() == held
		received := mail.GetToAgentId() == held

		if req.Msg.GetDirection() == agentv1.MailDirection_MAIL_DIRECTION_SENT && sent {
			found = append(found, mail)

			continue
		}

		if req.Msg.GetDirection() != agentv1.MailDirection_MAIL_DIRECTION_SENT && received {
			found = append(found, mail)
		}
	}

	return connect.NewResponse(&agentv1.ListMailResponse{Mail: found}), nil
}

// workflowService answers the panel's reads about workflows out of the scenario.
type workflowService struct {
	workflowv1connect.UnimplementedWorkflowServiceHandler
	held scenario.Scenario
}

func (s workflowService) ListWorkflows(
	_ context.Context,
	req *connect.Request[workflowv1.ListWorkflowsRequest],
) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	if req.Msg.GetArchived() {
		return connect.NewResponse(&workflowv1.ListWorkflowsResponse{}), nil
	}

	return connect.NewResponse(&workflowv1.ListWorkflowsResponse{Workflows: s.held.Workflows}), nil
}

// scheduleService answers the panel's reads about schedules out of the scenario.
type scheduleService struct {
	schedulev1connect.UnimplementedScheduleServiceHandler
	held scenario.Scenario
}

func (s scheduleService) ListSchedules(
	_ context.Context,
	_ *connect.Request[schedulev1.ListSchedulesRequest],
) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	return connect.NewResponse(&schedulev1.ListSchedulesResponse{Schedules: s.held.Schedules}), nil
}

// hookService answers the panel's reads about hooks, of which a scenario has none.
type hookService struct {
	hookv1connect.UnimplementedHookServiceHandler
}

func (hookService) ListHooks(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[hookv1.ListHooksResponse], error) {
	return connect.NewResponse(&hookv1.ListHooksResponse{}), nil
}

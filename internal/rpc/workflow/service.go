// Package workflow serves WorkflowService over ConnectRPC.
package workflow

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/agents"
	"weave/lib/pagination"
	workflowv1 "weave/proto/gen/workflow/v1"

	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// workflowManager is the part of the workflow service the handlers drive. The
// handlers translate and nothing more. What a workflow may be lives behind this.
type workflowManager interface {
	NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error
	ArchiveWorkflow(ctx context.Context, id string) error
}

// workflowLister is the read side, kept separate because listing pages straight
// out of the store rather than through the service.
type workflowLister interface {
	ListPage(ctx context.Context, limit int64, cursor *workflowstore.Cursor) ([]workflowstore.Workflow, error)
}

// Service implements workflowv1connect.WorkflowServiceHandler.
type Service struct {
	lister    workflowLister
	workflows workflowManager
}

// NewService constructs the handler from the store it lists through and the
// service that owns the rules.
func NewService(lister workflowLister, workflows workflowManager) *Service {
	return &Service{lister: lister, workflows: workflows}
}

// ListWorkflows returns one page of workflows, newest first.
func (s *Service) ListWorkflows(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	paginator, err := pagination.New[workflowstore.Cursor, workflow](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	stored, err := s.lister.ListPage(ctx, paginator.Limit(), paginator.Cursor())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list workflows: %w", err))
	}

	resources := make([]workflow, 0, len(stored))
	for _, w := range stored {
		resources = append(resources, renderWorkflow(w))
	}

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &workflowv1.ListWorkflowsResponse{NextPageToken: next}
	for _, resource := range resources {
		response.Workflows = append(response.Workflows, resource.Workflow)
	}

	return connect.NewResponse(response), nil
}

// GetWorkflow returns one workflow by id.
func (s *Service) GetWorkflow(ctx context.Context, req *connect.Request[workflowv1.GetWorkflowRequest]) (*connect.Response[workflowv1.GetWorkflowResponse], error) {
	found, err := s.workflows.GetWorkflow(ctx, req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get workflow: %w", err))
	}
	if found == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&workflowv1.GetWorkflowResponse{Workflow: renderWorkflow(*found).Workflow}), nil
}

// CreateWorkflow adds a workflow and returns it as stored.
func (s *Service) CreateWorkflow(ctx context.Context, req *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	created, err := s.workflows.NewWorkflow(ctx, workflowstore.Config{
		Name:          req.Msg.GetName(),
		Context:       req.Msg.GetContext(),
		Harness:       req.Msg.GetHarness(),
		Cwd:           req.Msg.GetCwd(),
		Flags:         req.Msg.GetFlags(),
		SetupAction:   req.Msg.GetSetupAction(),
		CleanupAction: req.Msg.GetCleanupAction(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create workflow: %w", err))
	}

	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: renderWorkflow(*created).Workflow}), nil
}

// UpdateWorkflow replaces a workflow's editable fields.
func (s *Service) UpdateWorkflow(ctx context.Context, req *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.workflows.UpdateWorkflow(ctx, req.Msg.GetId(), workflowstore.Config{
		Name:          req.Msg.GetName(),
		Context:       req.Msg.GetContext(),
		Harness:       req.Msg.GetHarness(),
		Cwd:           req.Msg.GetCwd(),
		Flags:         req.Msg.GetFlags(),
		SetupAction:   req.Msg.GetSetupAction(),
		CleanupAction: req.Msg.GetCleanupAction(),
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update workflow: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveWorkflow hides a workflow and archives the agents inside it.
func (s *Service) ArchiveWorkflow(ctx context.Context, req *connect.Request[workflowv1.ArchiveWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.workflows.ArchiveWorkflow(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	case errors.Is(err, agents.ErrWorkflowHasSchedules):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to archive workflow: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

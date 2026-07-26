package rpc

import (
	"context"
	"errors"
	"fmt"

	"claude-agents/internal/agents"
	"claude-agents/internal/store"
	"claude-agents/lib/pagination"
	workflowv1 "claude-agents/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// workflowManager is the part of the workflow service the handlers drive. The
// handlers translate and nothing more; what a workflow is allowed to be lives
// behind this.
type workflowManager interface {
	NewWorkflow(cfg store.WorkflowConfig) (*store.Workflow, error)
	GetWorkflow(id string) (*store.Workflow, error)
	UpdateWorkflowByID(id string, cfg store.WorkflowConfig) error
	DeleteWorkflowByID(id string, archiveChats bool) error
}

// workflowLister is the read side, kept separate because listing pages straight
// out of the store rather than through the service.
type workflowLister interface {
	ListWorkflowsPage(ctx context.Context, limit int64, cursor *store.WorkflowCursor) ([]store.Workflow, error)
}

// WorkflowService implements workflowv1connect.WorkflowServiceHandler.
type WorkflowService struct {
	store     workflowLister
	workflows workflowManager
}

// NewWorkflowService constructs the handler from the store it lists through and
// the service that owns the rules.
func NewWorkflowService(s workflowLister, workflows workflowManager) *WorkflowService {
	return &WorkflowService{store: s, workflows: workflows}
}

// ListWorkflows returns one page of workflows, most recently updated first.
func (s *WorkflowService) ListWorkflows(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	paginator, err := pagination.New[store.WorkflowCursor, workflow](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	stored, err := s.store.ListWorkflowsPage(ctx, paginator.Limit(), paginator.Cursor())
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
func (s *WorkflowService) GetWorkflow(_ context.Context, req *connect.Request[workflowv1.GetWorkflowRequest]) (*connect.Response[workflowv1.GetWorkflowResponse], error) {
	found, err := s.workflows.GetWorkflow(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get workflow: %w", err))
	}
	if found == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&workflowv1.GetWorkflowResponse{Workflow: renderWorkflow(*found).Workflow}), nil
}

// CreateWorkflow adds a workflow and returns it as stored.
func (s *WorkflowService) CreateWorkflow(_ context.Context, req *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	created, err := s.workflows.NewWorkflow(store.WorkflowConfig{
		Name:    req.Msg.GetName(),
		Context: req.Msg.GetContext(),
		Cwd:     req.Msg.GetCwd(),
		Flags:   req.Msg.GetFlags(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create workflow: %w", err))
	}

	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: renderWorkflow(*created).Workflow}), nil
}

// UpdateWorkflow replaces a workflow's editable fields.
func (s *WorkflowService) UpdateWorkflow(_ context.Context, req *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.workflows.UpdateWorkflowByID(req.Msg.GetId(), store.WorkflowConfig{
		Name:    req.Msg.GetName(),
		Context: req.Msg.GetContext(),
		Cwd:     req.Msg.GetCwd(),
		Flags:   req.Msg.GetFlags(),
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update workflow: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteWorkflow removes a workflow, orphaning or archiving its chats.
func (s *WorkflowService) DeleteWorkflow(_ context.Context, req *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.workflows.DeleteWorkflowByID(req.Msg.GetId(), req.Msg.GetArchiveChats())
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete workflow: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

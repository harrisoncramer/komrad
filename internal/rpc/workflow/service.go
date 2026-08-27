package workflow

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/agents"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	workflowv1 "weave/proto/gen/workflow/v1"

	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type workflowWriter interface {
	NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error
	ArchiveWorkflow(ctx context.Context, id string) error
	RestoreWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	DeleteWorkflow(ctx context.Context, id string) error
}

type workflowLister interface {
	ListPage(ctx context.Context, limit int64, cursor *workflowstore.Cursor, archived bool) ([]workflowstore.Workflow, error)
}

type Service struct {
	lister    workflowLister
	workflows workflowWriter
}

// NewService constructs the handler from the store it lists through and the
// service that owns the rules.
func NewService(lister workflowLister, workflows workflowWriter) *Service {
	return &Service{lister: lister, workflows: workflows}
}

// ListWorkflows returns one page of workflows, newest first.
func (s *Service) ListWorkflows(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	paginator, err := pagination.New[workflowstore.Cursor, workflow](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	stored, err := s.lister.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.Msg.GetArchived())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list workflows: %w", err))
	}

	resources := xslice.Map(stored, renderWorkflow)

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &workflowv1.ListWorkflowsResponse{NextPageToken: next}
	response.Workflows = xslice.Map(resources, func(resource workflow) *workflowv1.Workflow { return resource.Workflow })

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
		Worktree:      worktreeConfigFromProto(req.Msg.GetWorktree()),
	})
	switch {
	case errors.Is(err, agents.ErrWorktreeNeedsCwd), errors.Is(err, agents.ErrWorktreeOwnsActions), errors.Is(err, agents.ErrWorktreeActionsReserved):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
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
		Worktree:      worktreeConfigFromProto(req.Msg.GetWorktree()),
	})
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no workflow with id %q", req.Msg.GetId()))
	case errors.Is(err, agents.ErrWorktreeNeedsCwd), errors.Is(err, agents.ErrWorktreeOwnsActions), errors.Is(err, agents.ErrWorktreeActionsReserved):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update workflow: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func worktreeConfigFromProto(w *workflowv1.WorktreeConfig) workflowstore.WorktreeConfig {
	return workflowstore.WorktreeConfig{
		Enabled:      w.GetEnabled(),
		BaseBranch:   w.GetBaseBranch(),
		CopyPaths:    w.GetCopyPaths(),
		SymlinkPaths: w.GetSymlinkPaths(),
		DiffExclude:  w.GetDiffExclude(),
	}
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

// RestoreWorkflow brings a workflow back with the agents archived inside it.
func (s *Service) RestoreWorkflow(ctx context.Context, req *connect.Request[workflowv1.RestoreWorkflowRequest]) (*connect.Response[workflowv1.RestoreWorkflowResponse], error) {
	restored, err := s.workflows.RestoreWorkflow(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no archived workflow with id %q", req.Msg.GetId()))
	case errors.Is(err, agents.ErrTooManyAgents):
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	case errors.Is(err, store.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restore workflow: %w", err))
	}

	return connect.NewResponse(&workflowv1.RestoreWorkflowResponse{Workflow: renderWorkflow(*restored).Workflow}), nil
}

// DeleteWorkflow permanently removes an archived workflow, the archived agents still inside
// it, and the archived schedules that launched into it.
func (s *Service) DeleteWorkflow(ctx context.Context, req *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.workflows.DeleteWorkflow(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrWorkflowNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no archived workflow with id %q", req.Msg.GetId()))
	case errors.Is(err, agents.ErrScheduleHasAgents):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete workflow: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

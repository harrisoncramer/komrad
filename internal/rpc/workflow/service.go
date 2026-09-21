// Package workflow serves the workflow RPCs, which create, edit, archive and restore the groups
// agents are filed under.
package workflow

import (
	"context"

	"weave/internal/fail"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/lib/pagination"
	"weave/lib/xslice"
	workflowv1 "weave/proto/gen/workflow/v1"

	markstore "weave/internal/store/mark"
	workflowstore "weave/internal/store/workflow"

	"google.golang.org/protobuf/types/known/emptypb"
)

type workflowReadWriter interface {
	NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error
	ArchiveWorkflow(ctx context.Context, id string) error
	RestoreWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	DeleteWorkflow(ctx context.Context, id string) error
}

// markReader is the letters a person addresses rows by, which a workflow carries so the panel
// can draw one on its header.
type markReader interface {
	ListMarks(ctx context.Context) ([]markstore.Mark, error)
}

type workflowLister interface {
	ListPage(ctx context.Context, limit int64, cursor *workflowstore.Cursor, archived bool) ([]workflowstore.Workflow, error)
}

type WorkflowService struct {
	lister    workflowLister
	workflows workflowReadWriter
	marks     markReader
}

// NewWorkflowService constructs the handler from the store it lists through, the service that owns the
// rules, and the marks a workflow header draws.
func NewWorkflowService(lister workflowLister, workflows workflowReadWriter, marks markReader) *WorkflowService {
	return &WorkflowService{
		lister:    lister,
		workflows: workflows,
		marks:     marks,
	}
}

// ListWorkflows returns one page of workflows, by name.
func (s *WorkflowService) ListWorkflows(ctx context.Context, req *workflowv1.ListWorkflowsRequest) (*workflowv1.ListWorkflowsResponse, *fail.Error) {
	paginator, err := pagination.New[workflowstore.Cursor, workflow](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	stored, err := s.lister.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.GetArchived())
	if err != nil {
		return nil, rpcerr.From(err, "failed to list workflows")
	}

	marks, failure := s.marksByWorkflow(ctx)
	if failure != nil {
		return nil, failure
	}

	view := workflowViewOptions(req.GetView())
	resources := xslice.Map(stored, func(w workflowstore.Workflow) workflow { return renderWorkflow(w, marks[w.ID], view...) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	response := &workflowv1.ListWorkflowsResponse{NextPageToken: next}
	response.Workflows = xslice.Map(resources, func(resource workflow) *workflowv1.Workflow { return resource.Workflow })

	return response, nil
}

// GetWorkflow returns one workflow by id.
func (s *WorkflowService) GetWorkflow(ctx context.Context, req *workflowv1.GetWorkflowRequest) (*workflowv1.GetWorkflowResponse, *fail.Error) {
	found, err := s.workflows.GetWorkflow(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get workflow")
	}

	mark, failure := s.markOf(ctx, found.ID)
	if failure != nil {
		return nil, failure
	}

	return &workflowv1.GetWorkflowResponse{Workflow: renderWorkflow(*found, mark).Workflow}, nil
}

// CreateWorkflow adds a workflow and returns it as stored.
func (s *WorkflowService) CreateWorkflow(ctx context.Context, req *workflowv1.CreateWorkflowRequest) (*workflowv1.CreateWorkflowResponse, *fail.Error) {
	created, err := s.workflows.NewWorkflow(ctx, workflowstore.Config{
		Name:     req.GetName(),
		Context:  req.GetContext(),
		Harness:  req.GetHarness(),
		Cwd:      req.GetCwd(),
		Flags:    req.GetFlags(),
		Worktree: render.WorktreeConfigFromProto(req.GetWorktree()),
		Hooks:    hooksFromProto(req.GetHooks()),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to create workflow")
	}

	return &workflowv1.CreateWorkflowResponse{Workflow: renderWorkflow(*created, nil).Workflow}, nil
}

// UpdateWorkflow replaces a workflow's editable fields.
func (s *WorkflowService) UpdateWorkflow(ctx context.Context, req *workflowv1.UpdateWorkflowRequest) (*emptypb.Empty, *fail.Error) {
	err := s.workflows.UpdateWorkflow(ctx, req.GetId(), workflowstore.Config{
		Name:     req.GetName(),
		Context:  req.GetContext(),
		Harness:  req.GetHarness(),
		Cwd:      req.GetCwd(),
		Flags:    req.GetFlags(),
		Worktree: render.WorktreeConfigFromProto(req.GetWorktree()),
		Hooks:    hooksFromProto(req.GetHooks()),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to update workflow")
	}

	return &emptypb.Empty{}, nil
}

func hooksFromProto(hooks []*workflowv1.WorkflowHook) []workflowstore.Hook {
	return xslice.Map(hooks, func(hook *workflowv1.WorkflowHook) workflowstore.Hook {
		return workflowstore.Hook{Event: hook.GetEvent(), Script: hook.GetScript()}
	})
}

// ArchiveWorkflow hides a workflow and archives the agents inside it.
func (s *WorkflowService) ArchiveWorkflow(ctx context.Context, req *workflowv1.ArchiveWorkflowRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.workflows.ArchiveWorkflow(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to archive workflow")
	}

	return &emptypb.Empty{}, nil
}

// RestoreWorkflow brings a workflow back with the agents archived inside it.
func (s *WorkflowService) RestoreWorkflow(ctx context.Context, req *workflowv1.RestoreWorkflowRequest) (*workflowv1.RestoreWorkflowResponse, *fail.Error) {
	restored, err := s.workflows.RestoreWorkflow(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to restore workflow")
	}

	return &workflowv1.RestoreWorkflowResponse{Workflow: renderWorkflow(*restored, nil).Workflow}, nil
}

// DeleteWorkflow permanently removes an archived workflow, the archived agents still inside
// it, and the archived schedules that launched into it.
func (s *WorkflowService) DeleteWorkflow(ctx context.Context, req *workflowv1.DeleteWorkflowRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.workflows.DeleteWorkflow(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to delete workflow")
	}

	return &emptypb.Empty{}, nil
}

// marksByWorkflow is the letter each workflow answers to, keyed by its id.
func (s *WorkflowService) marksByWorkflow(ctx context.Context) (map[string]*string, *fail.Error) {
	listed, err := s.marks.ListMarks(ctx)
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the marks")
	}

	by := make(map[string]*string, len(listed))
	for _, mark := range listed {
		if mark.WorkflowID != nil {
			by[*mark.WorkflowID] = &mark.Mark
		}
	}

	return by, nil
}

// markOf is the letter one workflow answers to, or nothing when it holds none.
func (s *WorkflowService) markOf(ctx context.Context, id string) (*string, *fail.Error) {
	by, failure := s.marksByWorkflow(ctx)
	if failure != nil {
		return nil, failure
	}

	return by[id], nil
}

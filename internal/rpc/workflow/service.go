// Package workflow serves the workflow RPCs, which create, edit, archive and restore the groups
// agents are filed under.
package workflow

import (
	"context"

	"weave/internal/agents"
	"weave/internal/rpcerr"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	workflowv1 "weave/proto/gen/workflow/v1"

	markstore "weave/internal/store/mark"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
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

type Service struct {
	lister    workflowLister
	workflows workflowReadWriter
	marks     markReader
}

// NewService constructs the handler from the store it lists through, the service that owns the
// rules, and the marks a workflow header draws.
func NewService(lister workflowLister, workflows workflowReadWriter, marks markReader) *Service {
	return &Service{
		lister:    lister,
		workflows: workflows,
		marks:     marks,
	}
}

// ListWorkflows returns one page of workflows, by name.
func (s *Service) ListWorkflows(ctx context.Context, req *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	paginator, err := pagination.New[workflowstore.Cursor, workflow](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	stored, err := s.lister.ListPage(ctx, paginator.Limit(), paginator.Cursor(), req.Msg.GetArchived())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list workflows")
	}

	marks, err := s.marksByWorkflow(ctx)
	if err != nil {
		return nil, err
	}

	view := workflowViewOptions(req.Msg.GetView())
	resources := xslice.Map(stored, func(w workflowstore.Workflow) workflow { return renderWorkflow(w, marks[w.ID], view...) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to build the next page token")
	}

	response := &workflowv1.ListWorkflowsResponse{NextPageToken: next}
	response.Workflows = xslice.Map(resources, func(resource workflow) *workflowv1.Workflow { return resource.Workflow })

	return connect.NewResponse(response), nil
}

// GetWorkflow returns one workflow by id.
func (s *Service) GetWorkflow(ctx context.Context, req *connect.Request[workflowv1.GetWorkflowRequest]) (*connect.Response[workflowv1.GetWorkflowResponse], error) {
	found, err := s.workflows.GetWorkflow(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to get workflow",
			rpcerr.Saying(store.ErrNotFound, "no workflow with id %q", req.Msg.GetId()))
	}

	mark, err := s.markOf(ctx, found.ID)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&workflowv1.GetWorkflowResponse{Workflow: renderWorkflow(*found, mark).Workflow}), nil
}

// CreateWorkflow adds a workflow and returns it as stored.
func (s *Service) CreateWorkflow(ctx context.Context, req *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	created, err := s.workflows.NewWorkflow(ctx, workflowstore.Config{
		Name:              req.Msg.GetName(),
		Context:           req.Msg.GetContext(),
		Harness:           req.Msg.GetHarness(),
		Cwd:               req.Msg.GetCwd(),
		Flags:             req.Msg.GetFlags(),
		Worktree:          worktreeConfigFromProto(req.Msg.GetWorktree()),
		Hooks:             hooksFromProto(req.Msg.GetHooks()),
		ArchiveOnPRClosed: req.Msg.GetArchiveOnPrClosed(),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to create workflow")
	}

	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: renderWorkflow(*created, nil).Workflow}), nil
}

// UpdateWorkflow replaces a workflow's editable fields.
func (s *Service) UpdateWorkflow(ctx context.Context, req *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.workflows.UpdateWorkflow(ctx, req.Msg.GetId(), workflowstore.Config{
		Name:              req.Msg.GetName(),
		Context:           req.Msg.GetContext(),
		Harness:           req.Msg.GetHarness(),
		Cwd:               req.Msg.GetCwd(),
		Flags:             req.Msg.GetFlags(),
		Worktree:          worktreeConfigFromProto(req.Msg.GetWorktree()),
		Hooks:             hooksFromProto(req.Msg.GetHooks()),
		ArchiveOnPRClosed: req.Msg.GetArchiveOnPrClosed(),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to update workflow",
			rpcerr.Saying(agents.ErrWorkflowNotFound, "no workflow with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func hooksFromProto(hooks []*workflowv1.WorkflowHook) []workflowstore.Hook {
	return xslice.Map(hooks, func(hook *workflowv1.WorkflowHook) workflowstore.Hook {
		return workflowstore.Hook{Event: hook.GetEvent(), Script: hook.GetScript()}
	})
}

func worktreeConfigFromProto(w *workflowv1.WorktreeConfig) workflowstore.WorktreeConfig {
	return workflowstore.WorktreeConfig{
		Enabled:      w.GetEnabled(),
		BaseBranch:   w.GetBaseBranch(),
		Dir:          w.GetDir(),
		CopyPaths:    w.GetCopyPaths(),
		SymlinkPaths: w.GetSymlinkPaths(),
		DiffExclude:  w.GetDiffExclude(),
	}
}

// ArchiveWorkflow hides a workflow and archives the agents inside it.
func (s *Service) ArchiveWorkflow(ctx context.Context, req *connect.Request[workflowv1.ArchiveWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.workflows.ArchiveWorkflow(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to archive workflow",
			rpcerr.Saying(agents.ErrWorkflowNotFound, "no workflow with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RestoreWorkflow brings a workflow back with the agents archived inside it.
func (s *Service) RestoreWorkflow(ctx context.Context, req *connect.Request[workflowv1.RestoreWorkflowRequest]) (*connect.Response[workflowv1.RestoreWorkflowResponse], error) {
	restored, err := s.workflows.RestoreWorkflow(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to restore workflow",
			rpcerr.Saying(agents.ErrWorkflowNotFound, "no archived workflow with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&workflowv1.RestoreWorkflowResponse{Workflow: renderWorkflow(*restored, nil).Workflow}), nil
}

// DeleteWorkflow permanently removes an archived workflow, the archived agents still inside
// it, and the archived schedules that launched into it.
func (s *Service) DeleteWorkflow(ctx context.Context, req *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.workflows.DeleteWorkflow(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to delete workflow",
			rpcerr.Saying(agents.ErrWorkflowNotFound, "no archived workflow with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// marksByWorkflow is the letter each workflow answers to, keyed by its id.
func (s *Service) marksByWorkflow(ctx context.Context) (map[string]*string, error) {
	listed, err := s.marks.ListMarks(ctx)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list the marks")
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
func (s *Service) markOf(ctx context.Context, id string) (*string, error) {
	by, err := s.marksByWorkflow(ctx)
	if err != nil {
		return nil, err
	}

	return by[id], nil
}

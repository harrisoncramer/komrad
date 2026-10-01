// Package workflow serves the workflow RPCs, which create, edit, archive and restore the groups
// agents are filed under.
package workflow

import (
	"context"

	"komrad/internal/agents"
	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/lib/pagination"
	"komrad/lib/xslice"
	workflowv1 "komrad/proto/gen/workflow/v1"

	workflowstore "komrad/internal/store/workflow"

	"google.golang.org/protobuf/types/known/emptypb"
)

type workflowReadWriter interface {
	NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error
	ArchiveWorkflow(ctx context.Context, id string) error
	RestoreWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error)
	DeleteWorkflow(ctx context.Context, id string) error
	ListWorkflows(ctx context.Context, params agents.ListWorkflowsParams) ([]workflowstore.Workflow, error)
}

type workflowConfigGetter interface {
	GetName() string
	GetContext() string
	GetHarness() string
	GetCwd() string
	GetFlags() string
	GetReviewerHarness() string
	GetReviewerFlags() string
	GetWorktree() *workflowv1.WorktreeConfig
	GetHooks() []*workflowv1.WorkflowHook
}

type NewHandlerParams struct {
	Workflows workflowReadWriter
}

type Handler struct {
	workflows workflowReadWriter
}

// NewHandler constructs the handler over the service that owns the rules.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{workflows: params.Workflows}
}

// ListWorkflows returns one page of workflows, by name.
func (s *Handler) ListWorkflows(ctx context.Context, req *workflowv1.ListWorkflowsRequest) (*workflowv1.ListWorkflowsResponse, *fail.Error) {
	paginator, err := pagination.New[workflowstore.Cursor, workflow](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	stored, err := s.workflows.ListWorkflows(ctx, agents.ListWorkflowsParams{
		Limit:    paginator.Limit(),
		Cursor:   paginator.Cursor(),
		Archived: req.GetArchived(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list workflows")
	}

	view := workflowViewOptions(req.GetView())
	resources := xslice.Map(stored, func(w workflowstore.Workflow) workflow { return renderWorkflow(w, view) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	response := &workflowv1.ListWorkflowsResponse{NextPageToken: next}
	response.Workflows = xslice.Map(resources, func(resource workflow) *workflowv1.Workflow { return resource.Workflow })

	return response, nil
}

// GetWorkflow returns one workflow by id.
func (s *Handler) GetWorkflow(ctx context.Context, req *workflowv1.GetWorkflowRequest) (*workflowv1.GetWorkflowResponse, *fail.Error) {
	found, err := s.workflows.GetWorkflow(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get workflow")
	}

	return &workflowv1.GetWorkflowResponse{Workflow: renderWorkflow(*found, render.WorkflowOptions{}).Workflow}, nil
}

// CreateWorkflow adds a workflow and returns it as stored.
func (s *Handler) CreateWorkflow(ctx context.Context, req *workflowv1.CreateWorkflowRequest) (*workflowv1.CreateWorkflowResponse, *fail.Error) {
	cfg := buildConfigFromRequest(req)
	cfg.EventPrompts = buildCreatedEventPrompts(req.GetEventPrompts())

	created, err := s.workflows.NewWorkflow(ctx, cfg)
	if err != nil {
		return nil, rpcerr.From(err, "failed to create workflow")
	}

	return &workflowv1.CreateWorkflowResponse{Workflow: renderWorkflow(*created, render.WorkflowOptions{}).Workflow}, nil
}

// UpdateWorkflow replaces a workflow's editable fields.
func (s *Handler) UpdateWorkflow(ctx context.Context, req *workflowv1.UpdateWorkflowRequest) (*emptypb.Empty, *fail.Error) {
	cfg := buildConfigFromRequest(req)
	cfg.EventPrompts = buildEventPromptsFromProto(req.GetEventPrompts())

	err := s.workflows.UpdateWorkflow(ctx, req.GetId(), cfg)
	if err != nil {
		return nil, rpcerr.From(err, "failed to update workflow")
	}

	return &emptypb.Empty{}, nil
}

// ArchiveWorkflow hides a workflow and archives the agents inside it.
func (s *Handler) ArchiveWorkflow(ctx context.Context, req *workflowv1.ArchiveWorkflowRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.workflows.ArchiveWorkflow(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to archive workflow")
	}

	return &emptypb.Empty{}, nil
}

// RestoreWorkflow brings a workflow back with the agents archived inside it.
func (s *Handler) RestoreWorkflow(ctx context.Context, req *workflowv1.RestoreWorkflowRequest) (*workflowv1.RestoreWorkflowResponse, *fail.Error) {
	restored, err := s.workflows.RestoreWorkflow(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to restore workflow")
	}

	return &workflowv1.RestoreWorkflowResponse{Workflow: renderWorkflow(*restored, render.WorkflowOptions{}).Workflow}, nil
}

// DeleteWorkflow permanently removes an archived workflow, the archived agents still inside
// it, and the archived schedules that launched into it.
func (s *Handler) DeleteWorkflow(ctx context.Context, req *workflowv1.DeleteWorkflowRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.workflows.DeleteWorkflow(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to delete workflow")
	}

	return &emptypb.Empty{}, nil
}

func buildConfigFromRequest(req workflowConfigGetter) workflowstore.Config {
	return workflowstore.Config{
		Name:            req.GetName(),
		Context:         req.GetContext(),
		Harness:         req.GetHarness(),
		Cwd:             req.GetCwd(),
		Flags:           req.GetFlags(),
		ReviewerHarness: req.GetReviewerHarness(),
		ReviewerFlags:   req.GetReviewerFlags(),
		Worktree:        buildWorktreeConfigFromProto(req.GetWorktree()),
		Hooks:           buildHooksFromProto(req.GetHooks()),
	}
}

func buildHooksFromProto(hooks []*workflowv1.WorkflowHook) []workflowstore.Hook {
	return xslice.Map(hooks, func(hook *workflowv1.WorkflowHook) workflowstore.Hook {
		return workflowstore.Hook{Event: hook.GetEvent(), Script: hook.GetScript()}
	})
}

// buildCreatedEventPrompts is the prompts a new workflow starts with, each event's default when the
// caller sent none.
func buildCreatedEventPrompts(sent *workflowv1.WorkflowEventPrompts) []workflowstore.EventPrompt {
	if sent == nil {
		return xslice.Map(eventprompts.All(), func(event eventprompts.Event) workflowstore.EventPrompt {
			return workflowstore.EventPrompt{Event: event.Name, Prompt: event.DefaultPrompt}
		})
	}

	return buildEventPromptsFromProto(sent.GetPrompts())
}

func buildEventPromptsFromProto(prompts []*workflowv1.WorkflowEventPrompt) []workflowstore.EventPrompt {
	return xslice.Map(prompts, func(prompt *workflowv1.WorkflowEventPrompt) workflowstore.EventPrompt {
		return workflowstore.EventPrompt{Event: prompt.GetEvent(), Prompt: prompt.GetPrompt()}
	})
}

func buildWorktreeConfigFromProto(w *workflowv1.WorktreeConfig) workflowstore.WorktreeConfig {
	return workflowstore.WorktreeConfig{
		Enabled:      w.GetEnabled(),
		BaseBranch:   w.GetBaseBranch(),
		Dir:          w.GetDir(),
		CopyPaths:    w.GetCopyPaths(),
		SymlinkPaths: w.GetSymlinkPaths(),
		DiffExclude:  w.GetDiffExclude(),
	}
}

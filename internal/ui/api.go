package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "weave/proto/gen/worktree/v1"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	"weave/lib/xslice"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

const requestTimeout = 10 * time.Second

const createAgentTimeout = 11 * time.Minute

type api struct {
	agents    agentv1connect.AgentServiceClient
	workflows workflowv1connect.WorkflowServiceClient
	schedules schedulev1connect.ScheduleServiceClient
	hooks     hookv1connect.HookServiceClient
	worktrees worktreev1connect.WorktreeServiceClient
}

// call runs work under its own deadline.
func call[T any](timeout time.Duration, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return fn(ctx)
}

// mutate is call for a request whose response the panel has no use for, under the ordinary
// request timeout.
func mutate(fn func(context.Context) (any, error)) error {
	_, err := call(requestTimeout, fn)

	return err
}

func (a api) renameAgent(agent panel.Agent, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == agent.Name {
		return nil
	}

	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
			Id:   agent.ID,
			Name: name,
		}))
	})
}

// markRunsRead claims the failed runs an agent is holding, which opening its run log counts as.
func (a api) markRunsRead(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.MarkRunsRead(ctx, connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: id}))
	})
}

type createAgentParams struct {
	Prompt           string
	Title            string
	Harness          string
	Dir              string
	Flags            string
	ProvisionEnabled *bool
	WorkflowID       string
	ParentAgentID    string
}

// nilIfBlank leaves an optional field unset rather than sending it empty, which the request's
// own validation would refuse.
func nilIfBlank(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func (a api) createAgent(params createAgentParams) (*agentv1.Agent, error) {
	return call(createAgentTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:           params.Prompt,
			Title:            params.Title,
			Harness:          params.Harness,
			Dir:              params.Dir,
			Flags:            params.Flags,
			ProvisionEnabled: params.ProvisionEnabled,
			WorkflowId:       params.WorkflowID,
			ParentAgentId:    nilIfBlank(params.ParentAgentID),
		}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetAgent(), nil
	})
}

type adoptParams struct {
	Ref        string
	Prompt     string
	Title      string
	Harness    string
	Flags      string
	WorkflowID string
}

func (a api) adopt(params adoptParams) (*agentv1.Agent, error) {
	return call(createAgentTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
			Ref:        params.Ref,
			Prompt:     params.Prompt,
			Title:      params.Title,
			Harness:    params.Harness,
			Flags:      params.Flags,
			WorkflowId: params.WorkflowID,
		}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetAgent(), nil
	})
}

func (a api) archiveAgents(ids []string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	})
}

func (a api) setHidden(agent panel.Agent, hidden bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: agent.ID, Hidden: hidden}))
	})
}

// hold stops an agent so the person may take its worktree.
func (a api) hold(agent panel.Agent) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: agent.ID}))
	})
}

// startOrchestrator brings a workflow's orchestrator back, starting the one it has never had.
func (a api) startOrchestrator(workflowID string) (*agentv1.Agent, error) {
	return call(createAgentTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.agents.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{
			WorkflowId: workflowID,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to start the orchestrator of workflow %q: %w", workflowID, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

// release lets go of a held agent, saying what changed while it was held.
func (a api) release(agent panel.Agent, said string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.ReleaseAgent(ctx, connect.NewRequest(&agentv1.ReleaseAgentRequest{
			Id:   agent.ID,
			Said: said,
		}))
	})
}

func (a api) setMark(target markTargetRef, mark string) error {
	return a.writeMark(markRequest(target, &mark))
}

func (a api) clearMark(target markTargetRef) error {
	return a.writeMark(markRequest(target, nil))
}

func (a api) writeMark(req *agentv1.SetMarkRequest) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.SetMark(ctx, connect.NewRequest(req))
	})
}

func markRequest(target markTargetRef, mark *string) *agentv1.SetMarkRequest {
	req := &agentv1.SetMarkRequest{Mark: mark}
	if target.workflowID != "" {
		req.Target = &agentv1.SetMarkRequest_WorkflowId{WorkflowId: target.workflowID}

		return req
	}

	req.Target = &agentv1.SetMarkRequest_AgentId{AgentId: target.agentID}

	return req
}

func (a api) restoreAgents(ids []string) error {
	return mutate(func(ctx context.Context) (any, error) {
		resp, err := a.agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
		if err != nil {
			return nil, fmt.Errorf("failed to restore %d agents: %w", len(ids), err)
		}

		return resp, nil
	})
}

type retryAgentParams struct {
	ID      string
	Flags   string
	Prompt  string
	Harness string
}

func (a api) retryAgent(params retryAgentParams) (*agentv1.Agent, error) {
	return call(createAgentTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.agents.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{
			Id:      params.ID,
			Flags:   params.Flags,
			Prompt:  params.Prompt,
			Harness: params.Harness,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to retry agent %q: %w", params.ID, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

func (a api) resumeAgent(id string) (*agentv1.Agent, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to resume agent %q: %w", id, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

func (a api) planRebase(id string) (*agentv1.PlanRebaseResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.PlanRebaseResponse, error) {
		resp, err := a.agents.PlanRebase(ctx, connect.NewRequest(&agentv1.PlanRebaseRequest{AgentId: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to work out what a rebase of agent %q would do: %w", id, err)
		}

		return resp.Msg, nil
	})
}

func (a api) rebaseWorktree(id string) (*agentv1.RebaseWorktreeResponse, error) {
	return call(rebaseTimeout, func(ctx context.Context) (*agentv1.RebaseWorktreeResponse, error) {
		resp, err := a.agents.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{AgentId: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to rebase the worktree of agent %q: %w", id, err)
		}

		return resp.Msg, nil
	})
}

func (a api) deleteAgents(ids []string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))
	})
}

func (a api) emptyArchive() error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))
	})
}

func (a api) getSchedule(id string) (*panel.Schedule, error) {
	return call(agentsTimeout, func(ctx context.Context) (*panel.Schedule, error) {
		resp, err := a.schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
		}

		schedule := scheduleFrom(resp.Msg.GetSchedule())

		return &schedule, nil
	})
}

func (a api) createSchedule(cfg panel.ScheduleConfig) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.schedules.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
			Name:       strings.TrimSpace(cfg.Name),
			Cron:       cfg.Cron,
			Timezone:   cfg.Timezone,
			Enabled:    cfg.Enabled,
			HideAgents: cfg.HideAgents,
			Prompt:     cfg.Prompt,
			WorkflowId: cfg.WorkflowID,
		}))
	})
}

func (a api) updateSchedule(id string, cfg panel.ScheduleConfig) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.schedules.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
			Id:         id,
			Name:       strings.TrimSpace(cfg.Name),
			Cron:       cfg.Cron,
			Timezone:   cfg.Timezone,
			Enabled:    cfg.Enabled,
			HideAgents: cfg.HideAgents,
			Prompt:     cfg.Prompt,
			WorkflowId: cfg.WorkflowID,
		}))
	})
}

func (a api) listScheduleRuns(id string) ([]panel.ScheduleRun, error) {
	return call(requestTimeout, func(ctx context.Context) ([]panel.ScheduleRun, error) {
		resp, err := a.schedules.ListScheduleRuns(ctx, connect.NewRequest(&schedulev1.ListScheduleRunsRequest{
			Id:    id,
			Limit: scheduleRunsShown,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the runs of schedule %q: %w", id, err)
		}

		return xslice.Map(resp.Msg.GetRuns(), scheduleRunFrom), nil
	})
}

func (a api) setScheduleEnabled(id string, enabled bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.schedules.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
			Id:      id,
			Enabled: enabled,
		}))
	})
}

func (a api) archiveSchedule(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.schedules.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: id}))
	})
}

func (a api) getWorkflow(id string) (*panel.Workflow, error) {
	return call(agentsTimeout, func(ctx context.Context) (*panel.Workflow, error) {
		resp, err := a.workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
		}

		workflow := workflowFrom(resp.Msg.GetWorkflow())

		return &workflow, nil
	})
}

func worktreeConfigToProto(w panel.WorktreeConfig) *workflowv1.WorktreeConfig {
	return &workflowv1.WorktreeConfig{
		Enabled:      w.Enabled,
		BaseBranch:   w.BaseBranch,
		Dir:          w.Dir,
		CopyPaths:    w.CopyPaths,
		SymlinkPaths: w.SymlinkPaths,
		DiffExclude:  w.DiffExclude,
	}
}

func hooksToProto(hooks []panel.Hook) []*workflowv1.WorkflowHook {
	return xslice.Map(hooks, func(hook panel.Hook) *workflowv1.WorkflowHook {
		return &workflowv1.WorkflowHook{Event: hook.Event, Script: hook.Script}
	})
}

func (a api) createWorkflow(cfg panel.WorkflowConfig) (*workflowv1.Workflow, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "workflow"
	}

	return call(requestTimeout, func(ctx context.Context) (*workflowv1.Workflow, error) {
		resp, err := a.workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
			Name:     name,
			Context:  cfg.Context,
			Harness:  cfg.Harness,
			Cwd:      cfg.Cwd,
			Flags:    cfg.Flags,
			Worktree: worktreeConfigToProto(cfg.Worktree),
			Hooks:    hooksToProto(cfg.Hooks),
		}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetWorkflow(), nil
	})
}

func (a api) updateWorkflow(w panel.Workflow) error {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil
	}

	return mutate(func(ctx context.Context) (any, error) {
		return a.workflows.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
			Id:       w.ID,
			Name:     name,
			Context:  w.Context,
			Harness:  w.Harness,
			Cwd:      w.Cwd,
			Flags:    w.Flags,
			Worktree: worktreeConfigToProto(w.Worktree),
			Hooks:    hooksToProto(w.Hooks),
		}))
	})
}

func (a api) archiveWorkflow(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))
	})
}

func (a api) restoreWorkflow(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		resp, err := a.workflows.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to restore workflow %q: %w", id, err)
		}

		return resp, nil
	})
}

func (a api) deleteWorkflow(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.workflows.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id}))
	})
}

func (a api) listHooks() ([]*hookv1.Hook, error) {
	return call(requestTimeout, func(ctx context.Context) ([]*hookv1.Hook, error) {
		resp, err := a.hooks.ListHooks(ctx, connect.NewRequest(&emptypb.Empty{}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetHooks(), nil
	})
}

func (a api) listWorktrees() (*worktreev1.ListWorktreesResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*worktreev1.ListWorktreesResponse, error) {
		resp, err := a.worktrees.ListWorktrees(ctx, connect.NewRequest(&emptypb.Empty{}))
		if err != nil {
			return nil, fmt.Errorf("failed to list worktrees: %w", err)
		}

		return resp.Msg, nil
	})
}

func (a api) getWorktree(dir string) (*worktreev1.GetWorktreeResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*worktreev1.GetWorktreeResponse, error) {
		resp, err := a.worktrees.GetWorktree(ctx, connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: dir}))
		if err != nil {
			return nil, fmt.Errorf("failed to read the worktree at %q: %w", dir, err)
		}

		return resp.Msg, nil
	})
}

func (a api) deleteWorktrees(dirs []string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.worktrees.DeleteWorktrees(ctx, connect.NewRequest(&worktreev1.DeleteWorktreesRequest{Dirs: dirs}))
	})
}

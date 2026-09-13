package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
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

func (a api) renameAgent(agent agentstore.Agent, name string) error {
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

// setViewed claims whatever the agent has finished up to now, or puts the mark back.
func (a api) setViewed(id string, viewed bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.SetAgentViewed(ctx, connect.NewRequest(&agentv1.SetAgentViewedRequest{Id: id, Viewed: viewed}))
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

func (a api) setHidden(agent agentstore.Agent, hidden bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: agent.ID, Hidden: hidden}))
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

func (a api) resumeAgent(id string) (*agentv1.Agent, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to resume agent %q: %w", id, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

func (a api) planRefresh(id string) (*agentv1.PlanRefreshResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.PlanRefreshResponse, error) {
		resp, err := a.agents.PlanRefresh(ctx, connect.NewRequest(&agentv1.PlanRefreshRequest{AgentId: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to work out what a rebase of agent %q would do: %w", id, err)
		}

		return resp.Msg, nil
	})
}

func (a api) rebaseWorktree(id string) (*agentv1.RebaseWorktreeResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.RebaseWorktreeResponse, error) {
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

func (a api) getSchedule(id string) (*schedulestore.Schedule, error) {
	return call(agentsTimeout, func(ctx context.Context) (*schedulestore.Schedule, error) {
		resp, err := a.schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
		}

		stored := scheduleFromProto(resp.Msg.GetSchedule())

		return &stored, nil
	})
}

func (a api) createSchedule(cfg schedulestore.Config) error {
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

func (a api) updateSchedule(id string, cfg schedulestore.Config) error {
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

func (a api) getWorkflow(id string) (*workflowstore.Workflow, error) {
	return call(agentsTimeout, func(ctx context.Context) (*workflowstore.Workflow, error) {
		resp, err := a.workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
		}

		workflow := workflowFrom(resp.Msg.GetWorkflow())

		return &workflow, nil
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

func worktreeConfigToProto(w workflowstore.WorktreeConfig) *workflowv1.WorktreeConfig {
	return &workflowv1.WorktreeConfig{
		Enabled:      w.Enabled,
		BaseBranch:   w.BaseBranch,
		Dir:          w.Dir,
		CopyPaths:    w.CopyPaths,
		SymlinkPaths: w.SymlinkPaths,
		DiffExclude:  w.DiffExclude,
	}
}

func hooksToProto(hooks []workflowstore.Hook) []*workflowv1.WorkflowHook {
	return xslice.Map(hooks, func(hook workflowstore.Hook) *workflowv1.WorkflowHook {
		return &workflowv1.WorkflowHook{Event: hook.Event, Script: hook.Script}
	})
}

func (a api) createWorkflow(cfg workflowstore.Config) (*workflowv1.Workflow, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "workflow"
	}

	return call(requestTimeout, func(ctx context.Context) (*workflowv1.Workflow, error) {
		resp, err := a.workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
			Name:              name,
			Context:           cfg.Context,
			Harness:           cfg.Harness,
			Cwd:               cfg.Cwd,
			Flags:             cfg.Flags,
			Worktree:          worktreeConfigToProto(cfg.Worktree),
			Hooks:             hooksToProto(cfg.Hooks),
			ArchiveOnPrClosed: cfg.ArchiveOnPRClosed,
		}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetWorkflow(), nil
	})
}

func (a api) updateWorkflow(w workflowstore.Workflow) error {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil
	}

	return mutate(func(ctx context.Context) (any, error) {
		return a.workflows.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
			Id:                w.ID,
			Name:              name,
			Context:           w.Context,
			Harness:           w.Harness,
			Cwd:               w.Cwd,
			Flags:             w.Flags,
			Worktree:          worktreeConfigToProto(w.Worktree),
			Hooks:             hooksToProto(w.Hooks),
			ArchiveOnPrClosed: w.ArchiveOnPRClosed,
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

func (a api) openPullRequest(agentID string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.hooks.OpenPullRequest(ctx, connect.NewRequest(&hookv1.OpenPullRequestRequest{
			AgentId: agentID,
		}))
	})
}

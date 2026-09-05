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

func (a api) renameAgent(agent agentstore.Agent, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == agent.Name {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
		Id:   agent.ID,
		Name: name,
	}))

	return err
}

// setViewed claims whatever the agent has finished up to now, or puts the mark back.
func (a api) setViewed(id string, viewed bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.SetAgentViewed(ctx, connect.NewRequest(&agentv1.SetAgentViewedRequest{Id: id, Viewed: viewed}))

	return err
}

// markRunsRead claims the failed runs an agent is holding, which is what opening its run log
// counts as.
func (a api) markRunsRead(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.MarkRunsRead(ctx, connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: id}))

	return err
}

type createAgentParams struct {
	Prompt           string
	Title            string
	Harness          string
	Dir              string
	Flags            string
	ProvisionEnabled *bool
	WorkflowID       string
	SetupPane        string
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
	ctx, cancel := context.WithTimeout(context.Background(), createAgentTimeout)
	defer cancel()

	resp, err := a.agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:           params.Prompt,
		Title:            params.Title,
		Harness:          params.Harness,
		Dir:              params.Dir,
		Flags:            params.Flags,
		ProvisionEnabled: params.ProvisionEnabled,
		WorkflowId:       params.WorkflowID,
		SetupPane:        params.SetupPane,
		ParentAgentId:    nilIfBlank(params.ParentAgentID),
	}))
	if err != nil {
		return nil, err
	}

	return resp.Msg.GetAgent(), nil
}

type adoptParams struct {
	Ref        string
	Prompt     string
	Title      string
	Harness    string
	Flags      string
	WorkflowID string
	SetupPane  string
}

func (a api) adopt(params adoptParams) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), createAgentTimeout)
	defer cancel()

	resp, err := a.agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
		Ref:        params.Ref,
		Prompt:     params.Prompt,
		Title:      params.Title,
		Harness:    params.Harness,
		Flags:      params.Flags,
		WorkflowId: params.WorkflowID,
		SetupPane:  params.SetupPane,
	}))
	if err != nil {
		return nil, err
	}

	return resp.Msg.GetAgent(), nil
}

func (a api) archiveAgents(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))

	return err
}

func (a api) setHidden(agent agentstore.Agent, hidden bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: agent.ID, Hidden: hidden}))

	return err
}

func (a api) restoreAgents(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if _, err := a.agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids})); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(ids), err)
	}

	return nil
}

func (a api) resumeAgent(id string) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := a.agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to resume agent %q: %w", id, err)
	}

	return resp.Msg.GetAgent(), nil
}

func (a api) deleteAgents(ids []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))

	return err
}

func (a api) emptyArchive() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.agents.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))

	return err
}

func (a api) getSchedule(id string) (*schedulestore.Schedule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()

	resp, err := a.schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	stored := scheduleFromProto(resp.Msg.GetSchedule())

	return &stored, nil
}

func (a api) createSchedule(cfg schedulestore.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
		Name:       strings.TrimSpace(cfg.Name),
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		HideAgents: cfg.HideAgents,
		Prompt:     cfg.Prompt,
		WorkflowId: cfg.WorkflowID,
	}))

	return err
}

func (a api) updateSchedule(id string, cfg schedulestore.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
		Id:         id,
		Name:       strings.TrimSpace(cfg.Name),
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		HideAgents: cfg.HideAgents,
		Prompt:     cfg.Prompt,
		WorkflowId: cfg.WorkflowID,
	}))

	return err
}

func (a api) setScheduleEnabled(id string, enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
		Id:      id,
		Enabled: enabled,
	}))

	return err
}

func (a api) archiveSchedule(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.schedules.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: id}))

	return err
}

func (a api) getWorkflow(id string) (*workflowstore.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()

	resp, err := a.workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFrom(resp.Msg.GetWorkflow())

	return &workflow, nil
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

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

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
}

func (a api) updateWorkflow(w workflowstore.Workflow) error {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.workflows.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
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

	return err
}

func (a api) archiveWorkflow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))

	return err
}

func (a api) restoreWorkflow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	if _, err := a.workflows.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: id})); err != nil {
		return fmt.Errorf("failed to restore workflow %q: %w", id, err)
	}

	return nil
}

func (a api) deleteWorkflow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.workflows.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id}))

	return err
}

func (a api) listHooks() ([]*hookv1.Hook, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	resp, err := a.hooks.ListHooks(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, err
	}

	return resp.Msg.GetHooks(), nil
}

func (a api) openPullRequest(agentID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	_, err := a.hooks.OpenPullRequest(ctx, connect.NewRequest(&hookv1.OpenPullRequestRequest{
		AgentId: agentID,
	}))

	return err
}

// Package api is the server the panel calls, and the writes more than one screen sends.
package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"komrad/internal/client"
	"komrad/internal/ui/panel"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	forgev1 "komrad/proto/gen/forge/v1"
	"komrad/proto/gen/forge/v1/forgev1connect"
	hookv1 "komrad/proto/gen/hook/v1"
	"komrad/proto/gen/hook/v1/hookv1connect"
	logv1 "komrad/proto/gen/log/v1"
	"komrad/proto/gen/log/v1/logv1connect"
	loopv1 "komrad/proto/gen/loop/v1"
	"komrad/proto/gen/loop/v1/loopv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	schedulev1 "komrad/proto/gen/schedule/v1"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"komrad/lib/xslice"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// RequestTimeout is how long a call to the server waits for its answer.
const RequestTimeout = 10 * time.Second

const (
	mergeTimeout     = 30 * time.Second
	autoMergeTimeout = 30 * time.Second
	draftTimeout     = 30 * time.Second
	rereviewTimeout  = 30 * time.Second
	rebaseTimeout    = 30 * time.Second
)

// pushTimeout outlasts the server's own limit on git, so the server is the one to give up.
const pushTimeout = 11 * time.Minute

// ErrNoSavedScreen is an agent whose pane was never saved.
var ErrNoSavedScreen = errors.New("no saved screen")

// Clients is the panel's connection to each of the server's services.
type Clients struct {
	Agents       agentv1connect.AgentServiceClient
	Workflows    workflowv1connect.WorkflowServiceClient
	Schedules    schedulev1connect.ScheduleServiceClient
	Hooks        hookv1connect.HookServiceClient
	Worktrees    worktreev1connect.WorktreeServiceClient
	Logs         logv1connect.LogServiceClient
	Forges       forgev1connect.ForgeServiceClient
	PullRequests pullrequestv1connect.PullRequestServiceClient
	Loops        loopv1connect.LoopServiceClient
	Tasks        taskv1connect.TaskServiceClient
}

// AgentReads is the part of the agent service a screen reads through.
type AgentReads interface {
	client.TranscriptGetter
	SearchTranscripts(ctx context.Context, req *connect.Request[agentv1.SearchTranscriptsRequest]) (*connect.Response[agentv1.SearchTranscriptsResponse], error)
	SearchDocuments(ctx context.Context, req *connect.Request[agentv1.SearchDocumentsRequest]) (*connect.Response[agentv1.SearchDocumentsResponse], error)
	ListReviewThreads(ctx context.Context, req *connect.Request[agentv1.ListReviewThreadsRequest]) (*connect.Response[agentv1.ListReviewThreadsResponse], error)
	OpenReviewHistory(ctx context.Context, req *connect.Request[agentv1.OpenReviewHistoryRequest]) (*connect.Response[agentv1.OpenReviewHistoryResponse], error)
	ListReviewViewedFiles(ctx context.Context, req *connect.Request[agentv1.ListReviewViewedFilesRequest]) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error)
}

// Call runs work under its own deadline.
func Call[T any](timeout time.Duration, fn func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return fn(ctx)
}

// mutate is call for a request whose response the panel has no use for, under the ordinary
// request timeout.
func mutate(fn func(context.Context) (any, error)) error {
	_, err := Call(RequestTimeout, fn)

	return err
}

func awaitHooks[T any](fn func(context.Context) (T, error)) (T, error) {
	return fn(context.Background()) //nolint:fussy // the server stops each hook at hooks.timeout and the transport drops a dead connection
}

func mutateAwaitingHooks(fn func(context.Context) (any, error)) error {
	_, err := awaitHooks(fn)

	return err
}

// RenameAgent renames an agent, leaving a blank or unchanged name alone.
func (a Clients) RenameAgent(agent panel.Agent, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == agent.Name {
		return nil
	}

	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
			Id:   agent.ID,
			Name: name,
		}))
	})
}

type CreateAgentParams struct {
	Prompt           string
	Title            string
	Harness          string
	Flags            string
	ProvisionEnabled *bool
	WorkflowID       string
	Task             string
	// DependsOn is the task the new task depends on, empty for one on the workflow's base branch.
	DependsOn string
}

// CreateAgent starts an agent.
func (a Clients) CreateAgent(params CreateAgentParams) (*agentv1.Agent, error) {
	return awaitHooks(func(ctx context.Context) (*agentv1.Agent, error) {
		req := &agentv1.CreateAgentRequest{
			Prompt:           params.Prompt,
			Title:            params.Title,
			Harness:          params.Harness,
			Flags:            params.Flags,
			ProvisionEnabled: params.ProvisionEnabled,
			WorkflowId:       params.WorkflowID,
		}
		if params.Task != "" {
			req.Task = &agentv1.CreateAgentRequest_TaskId{TaskId: params.Task}
		}
		if params.DependsOn != "" {
			req.DependsOn = &agentv1.CreateAgentRequest_DependsOnId{DependsOnId: params.DependsOn}
		}

		resp, err := a.Agents.CreateAgent(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetAgent(), nil
	})
}

type AdoptParams struct {
	Ref        string
	Prompt     string
	Title      string
	Harness    string
	Flags      string
	WorkflowID string
}

// Adopt starts an agent on a branch that already exists.
func (a Clients) Adopt(params AdoptParams) (*agentv1.AdoptResponse, error) {
	return awaitHooks(func(ctx context.Context) (*agentv1.AdoptResponse, error) {
		resp, err := a.Agents.Adopt(ctx, connect.NewRequest(&agentv1.AdoptRequest{
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

		return resp.Msg, nil
	})
}

// ArchiveAgents archives the agents.
func (a Clients) ArchiveAgents(ids []string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	})
}

// ArchiveTask archives a task with its agents.
func (a Clients) ArchiveTask(id string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.Tasks.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: id}}))
	})
}

// UpdateTaskParams is what a task edit changes, a nil field left as it stands.
type UpdateTaskParams struct {
	ID             string
	Slug           *string
	Title          *string
	Goal           *string
	Notes          *string
	NotesUpdatedAt *string
}

// UpdateTask changes what the params name of a task.
func (a Clients) UpdateTask(params UpdateTaskParams) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Tasks.UpdateTask(ctx, connect.NewRequest(&taskv1.UpdateTaskRequest{
			Task:           &taskv1.UpdateTaskRequest_TaskId{TaskId: params.ID},
			Slug:           params.Slug,
			Title:          params.Title,
			Goal:           params.Goal,
			Notes:          params.Notes,
			NotesUpdatedAt: params.NotesUpdatedAt,
		}))
	})
}

// DeleteTasks deletes the tasks, and their records with them when asked.
func (a Clients) DeleteTasks(ids []string, records bool) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{TaskIds: ids, Records: records}))
	})
}

// PlanPullRequestMerge asks the forge how the pull request at the address could merge.
func (a Clients) PlanPullRequestMerge(address string) (*forgev1.PlanForgePullRequestMergeResponse, error) {
	return Call(mergeTimeout, func(ctx context.Context) (*forgev1.PlanForgePullRequestMergeResponse, error) {
		resp, err := a.Forges.PlanForgePullRequestMerge(ctx, connect.NewRequest(&forgev1.PlanForgePullRequestMergeRequest{Url: address}))
		if err != nil {
			return nil, err
		}

		return resp.Msg, nil
	})
}

// MergeParams is a pull request to merge and how, refused by the forge once its head has moved
// past headSHA.
type MergeParams struct {
	URL      string
	Method   forgev1.MergeMethod
	Headline *string
	Body     *string
	HeadSHA  string
}

// MergePullRequest asks the forge to merge the pull request.
func (a Clients) MergePullRequest(params MergeParams) error {
	_, err := Call(mergeTimeout, func(ctx context.Context) (any, error) {
		return a.Forges.MergeForgePullRequest(ctx, connect.NewRequest(&forgev1.MergeForgePullRequestRequest{
			Url:      params.URL,
			Method:   params.Method,
			Headline: params.Headline,
			Body:     params.Body,
			HeadSha:  params.HeadSHA,
		}))
	})

	return err
}

// SetPullRequestAutoMerge asks the forge to merge the pull request on its own once its checks and
// reviews pass, or to stop waiting to.
func (a Clients) SetPullRequestAutoMerge(params MergeParams, on bool) error {
	_, err := Call(autoMergeTimeout, func(ctx context.Context) (any, error) {
		return a.Forges.SetForgePullRequestAutoMerge(ctx, connect.NewRequest(&forgev1.SetForgePullRequestAutoMergeRequest{
			Url:      params.URL,
			HeadSha:  params.HeadSHA,
			On:       on,
			Method:   params.Method,
			Headline: params.Headline,
			Body:     params.Body,
		}))
	})

	return err
}

// SetPullRequestDraft asks the forge to mark the pull request at the address a draft or ready.
func (a Clients) SetPullRequestDraft(address string, draft bool) error {
	_, err := Call(draftTimeout, func(ctx context.Context) (any, error) {
		return a.Forges.SetForgePullRequestDraft(ctx, connect.NewRequest(&forgev1.SetForgePullRequestDraftRequest{Url: address, Draft: draft}))
	})

	return err
}

// RequestPullRequestRereview asks everyone who requested changes on the pull request at the
// address to review it again.
func (a Clients) RequestPullRequestRereview(address string) error {
	_, err := Call(rereviewTimeout, func(ctx context.Context) (any, error) {
		return a.Forges.RequestForgePullRequestRereview(ctx, connect.NewRequest(&forgev1.RequestForgePullRequestRereviewRequest{Url: address}))
	})

	return err
}

// SetTaskHidden hides a task or shows it again.
func (a Clients) SetTaskHidden(taskID string, hidden bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Tasks.SetTaskHidden(ctx, connect.NewRequest(&taskv1.SetTaskHiddenRequest{Task: &taskv1.SetTaskHiddenRequest_TaskId{TaskId: taskID}, Hidden: hidden}))
	})
}

// MarkRunsRead marks an agent's runs read.
func (a Clients) MarkRunsRead(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.MarkRunsRead(ctx, connect.NewRequest(&agentv1.MarkRunsReadRequest{Id: id}))
	})
}

// MarkTaskRunsRead marks the runs of a task's agents read.
func (a Clients) MarkTaskRunsRead(taskID string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Tasks.MarkTaskRunsRead(ctx, connect.NewRequest(&taskv1.MarkTaskRunsReadRequest{Id: taskID}))
	})
}

// StartOrchestrator brings a workflow's orchestrator back, starting the one it has never had.
func (a Clients) StartOrchestrator(workflowID string) (*agentv1.Agent, error) {
	return awaitHooks(func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.Agents.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{
			WorkflowId: workflowID,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to start the orchestrator of workflow %q: %w", workflowID, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

// SendMail mails an agent from the person.
func (a Clients) SendMail(agentID, body string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.SendMail(ctx, connect.NewRequest(&agentv1.SendMailRequest{To: &agentv1.SendMailRequest_ToAgentId{ToAgentId: agentID}, Body: body}))
	})
}

// RestoreAgents brings archived agents back.
func (a Clients) RestoreAgents(ids []string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		resp, err := a.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{Ids: ids}))
		if err != nil {
			return nil, fmt.Errorf("failed to restore %d agents: %w", len(ids), err)
		}

		return resp, nil
	})
}

type RetryAgentParams struct {
	ID      string
	Flags   string
	Prompt  string
	Harness string
}

// RetryAgent retries an agent whose create failed.
func (a Clients) RetryAgent(params RetryAgentParams) (*agentv1.Agent, error) {
	return awaitHooks(func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.Agents.RetryAgent(ctx, connect.NewRequest(&agentv1.RetryAgentRequest{
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

type RestartAgentParams struct {
	ID      string
	Note    string
	Harness string
	Flags   string
}

// RestartAgent restarts a task's writer or a workflow's orchestrator on a new conversation.
func (a Clients) RestartAgent(params RestartAgentParams) (*agentv1.Agent, error) {
	return awaitHooks(func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.Agents.RestartAgent(ctx, connect.NewRequest(&agentv1.RestartAgentRequest{
			Id:      params.ID,
			Note:    params.Note,
			Harness: params.Harness,
			Flags:   params.Flags,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to restart agent %q: %w", params.ID, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

// ResumeAgent wakes a dormant agent.
func (a Clients) ResumeAgent(id string) (*agentv1.Agent, error) {
	return awaitHooks(func(ctx context.Context) (*agentv1.Agent, error) {
		resp, err := a.Agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to resume agent %q: %w", id, err)
		}

		return resp.Msg.GetAgent(), nil
	})
}

// PlanRebase asks what rebasing a task's worktree would do.
func (a Clients) PlanRebase(id string, ontoBase bool) (*agentv1.PlanRebaseResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*agentv1.PlanRebaseResponse, error) {
		resp, err := a.Agents.PlanRebase(ctx, connect.NewRequest(&agentv1.PlanRebaseRequest{Task: &agentv1.PlanRebaseRequest_TaskId{TaskId: id}, OntoBase: ontoBase}))
		if err != nil {
			return nil, fmt.Errorf("failed to work out what a rebase of task %q would do: %w", id, err)
		}

		return resp.Msg, nil
	})
}

// RebaseWorktree rebases a task's worktree.
func (a Clients) RebaseWorktree(id string, ontoBase bool) (*agentv1.RebaseWorktreeResponse, error) {
	return Call(rebaseTimeout, func(ctx context.Context) (*agentv1.RebaseWorktreeResponse, error) {
		resp, err := a.Agents.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{Task: &agentv1.RebaseWorktreeRequest_TaskId{TaskId: id}, OntoBase: ontoBase}))
		if err != nil {
			return nil, fmt.Errorf("failed to rebase the worktree of task %q: %w", id, err)
		}

		return resp.Msg, nil
	})
}

// PlanPush asks what pushing a task's worktree would do.
func (a Clients) PlanPush(id string) (*agentv1.PlanPushResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*agentv1.PlanPushResponse, error) {
		resp, err := a.Agents.PlanPush(ctx, connect.NewRequest(&agentv1.PlanPushRequest{Task: &agentv1.PlanPushRequest_TaskId{TaskId: id}}))
		if err != nil {
			return nil, fmt.Errorf("failed to work out what a push of task %q would do: %w", id, err)
		}

		return resp.Msg, nil
	})
}

// PushWorktree pushes a task's worktree.
func (a Clients) PushWorktree(id string) (*agentv1.PushWorktreeResponse, error) {
	return Call(pushTimeout, func(ctx context.Context) (*agentv1.PushWorktreeResponse, error) {
		resp, err := a.Agents.PushWorktree(ctx, connect.NewRequest(&agentv1.PushWorktreeRequest{Task: &agentv1.PushWorktreeRequest_TaskId{TaskId: id}}))
		if err != nil {
			return nil, fmt.Errorf("failed to push the branch of task %q: %w", id, err)
		}

		return resp.Msg, nil
	})
}

// AbortSequence aborts what a task's worktree has in progress.
func (a Clients) AbortSequence(id string) (*agentv1.AbortSequenceResponse, error) {
	return Call(rebaseTimeout, func(ctx context.Context) (*agentv1.AbortSequenceResponse, error) {
		resp, err := a.Agents.AbortSequence(ctx, connect.NewRequest(&agentv1.AbortSequenceRequest{Task: &agentv1.AbortSequenceRequest_TaskId{TaskId: id}}))
		if err != nil {
			return nil, fmt.Errorf("failed to abort what the worktree of task %q has in progress: %w", id, err)
		}

		return resp.Msg, nil
	})
}

// CreateSchedule creates a schedule.
func (a Clients) CreateSchedule(cfg panel.ScheduleConfig) error {
	return mutate(func(ctx context.Context) (any, error) {
		req := &schedulev1.CreateScheduleRequest{
			Name:       strings.TrimSpace(cfg.Name),
			Cron:       cfg.Cron,
			Timezone:   cfg.Timezone,
			Enabled:    cfg.Enabled,
			HideAgents: cfg.HideAgents,
			Prompt:     cfg.Prompt,
		}
		if cfg.Kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
			req.Target = &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: cfg.OrchestratorID}
		} else {
			req.Target = &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: cfg.WorkflowID}
		}

		return a.Schedules.CreateSchedule(ctx, connect.NewRequest(req))
	})
}

// UpdateSchedule rewrites a schedule, refusing a kind this build does not know.
func (a Clients) UpdateSchedule(id string, cfg panel.ScheduleConfig) error {
	if !panel.IsKnownScheduleKind(cfg.Kind) {
		return panel.ErrUnknownScheduleKind
	}

	return mutate(func(ctx context.Context) (any, error) {
		req := &schedulev1.UpdateScheduleRequest{
			Id:         id,
			Name:       strings.TrimSpace(cfg.Name),
			Cron:       cfg.Cron,
			Timezone:   cfg.Timezone,
			Enabled:    cfg.Enabled,
			HideAgents: cfg.HideAgents,
			Prompt:     cfg.Prompt,
		}
		if cfg.Kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
			req.Target = &schedulev1.UpdateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: cfg.OrchestratorID}
		} else {
			req.Target = &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: cfg.WorkflowID}
		}

		return a.Schedules.UpdateSchedule(ctx, connect.NewRequest(req))
	})
}

// SetScheduleEnabled turns a schedule on or off.
func (a Clients) SetScheduleEnabled(id string, enabled bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Schedules.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
			Id:      id,
			Enabled: enabled,
		}))
	})
}

// ArchiveSchedule archives a schedule.
func (a Clients) ArchiveSchedule(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Schedules.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: id}))
	})
}

// RestoreSchedule brings an archived schedule back.
func (a Clients) RestoreSchedule(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Schedules.RestoreSchedule(ctx, connect.NewRequest(&schedulev1.RestoreScheduleRequest{Id: id}))
	})
}

// DeleteSchedule deletes a schedule.
func (a Clients) DeleteSchedule(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Schedules.DeleteSchedule(ctx, connect.NewRequest(&schedulev1.DeleteScheduleRequest{Id: id}))
	})
}

func worktreeConfigToProto(w panel.WorktreeConfig) *workflowv1.WorktreeConfig {
	return &workflowv1.WorktreeConfig{
		Enabled:      w.Enabled,
		BaseBranch:   w.BaseBranch,
		Dir:          w.Dir,
		SetupCommand: w.SetupCommand,
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

func eventPromptsToProto(prompts []panel.EventPrompt) []*workflowv1.WorkflowEventPrompt {
	return xslice.Map(prompts, func(prompt panel.EventPrompt) *workflowv1.WorkflowEventPrompt {
		return &workflowv1.WorkflowEventPrompt{
			Event:  prompt.Event,
			Prompt: prompt.Prompt,
			Muted:  prompt.Muted,
		}
	})
}

// CreateWorkflow creates a workflow.
func (a Clients) CreateWorkflow(cfg panel.WorkflowConfig) (*workflowv1.Workflow, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "workflow"
	}

	return Call(RequestTimeout, func(ctx context.Context) (*workflowv1.Workflow, error) {
		resp, err := a.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
			Name:            name,
			Context:         cfg.Context,
			Harness:         cfg.Harness,
			Cwd:             cfg.Cwd,
			Flags:           cfg.Flags,
			ReviewerHarness: cfg.ReviewerHarness,
			ReviewerFlags:   cfg.ReviewerFlags,
			Worktree:        worktreeConfigToProto(cfg.Worktree),
			Hooks:           hooksToProto(cfg.Hooks),
			EventPrompts:    &workflowv1.WorkflowEventPrompts{Prompts: eventPromptsToProto(cfg.EventPrompts)},
		}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetWorkflow(), nil
	})
}

// UpdateWorkflow rewrites a workflow.
func (a Clients) UpdateWorkflow(w panel.Workflow) error {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil
	}

	return mutate(func(ctx context.Context) (any, error) {
		return a.Workflows.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
			Id:              w.ID,
			Name:            name,
			Context:         w.Context,
			Harness:         w.Harness,
			Cwd:             w.Cwd,
			Flags:           w.Flags,
			ReviewerHarness: w.ReviewerHarness,
			ReviewerFlags:   w.ReviewerFlags,
			Worktree:        worktreeConfigToProto(w.Worktree),
			Hooks:           hooksToProto(w.Hooks),
			EventPrompts:    eventPromptsToProto(w.EventPrompts),
		}))
	})
}

// ArchiveWorkflow archives a workflow.
func (a Clients) ArchiveWorkflow(id string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.Workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))
	})
}

// RestoreWorkflow brings an archived workflow back.
func (a Clients) RestoreWorkflow(id string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		resp, err := a.Workflows.RestoreWorkflow(ctx, connect.NewRequest(&workflowv1.RestoreWorkflowRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to restore workflow %q: %w", id, err)
		}

		return resp, nil
	})
}

// DeleteWorkflow deletes a workflow, and its records with it when asked.
func (a Clients) DeleteWorkflow(id string, records bool) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.Workflows.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id, Records: records}))
	})
}

// ListHooks lists the hooks the server knows.
func (a Clients) ListHooks() ([]*hookv1.Hook, error) {
	return Call(RequestTimeout, func(ctx context.Context) ([]*hookv1.Hook, error) {
		resp, err := a.Hooks.ListHooks(ctx, connect.NewRequest(&emptypb.Empty{}))
		if err != nil {
			return nil, err
		}

		return resp.Msg.GetHooks(), nil
	})
}

// GetWorktree inspects the worktree at a directory.
func (a Clients) GetWorktree(dir string) (*worktreev1.GetWorktreeResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*worktreev1.GetWorktreeResponse, error) {
		resp, err := a.Worktrees.GetWorktree(ctx, connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: dir}))
		if err != nil {
			return nil, fmt.Errorf("failed to read the worktree at %q: %w", dir, err)
		}

		return resp.Msg, nil
	})
}

// DeleteWorktrees deletes the worktrees at the directories.
func (a Clients) DeleteWorktrees(dirs []string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Worktrees.DeleteWorktrees(ctx, connect.NewRequest(&worktreev1.DeleteWorktreesRequest{Dirs: dirs}))
	})
}

// ListDocuments lists every document.
func (a Clients) ListDocuments() ([]*agentv1.Document, error) {
	return Call(RequestTimeout, func(ctx context.Context) ([]*agentv1.Document, error) {
		var listed []*agentv1.Document
		var cursor *string
		for {
			resp, err := a.Agents.ListDocuments(ctx, connect.NewRequest(&agentv1.ListDocumentsRequest{
				Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](100), Cursor: cursor},
			}))
			if err != nil {
				return nil, fmt.Errorf("failed to list documents: %w", err)
			}

			listed = append(listed, resp.Msg.GetDocuments()...)
			if resp.Msg.NextPageToken == nil {
				return listed, nil
			}
			cursor = resp.Msg.NextPageToken
		}
	})
}

// ListStatusChecks lists the status checks of a pull request.
func (a Clients) ListStatusChecks(pullRequestID string) (*pullrequestv1.ListStatusChecksResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*pullrequestv1.ListStatusChecksResponse, error) {
		resp, err := a.PullRequests.ListStatusChecks(ctx, connect.NewRequest(&pullrequestv1.ListStatusChecksRequest{PullRequestId: pullRequestID}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the status checks of pull request %q: %w", pullRequestID, err)
		}

		return resp.Msg, nil
	})
}

// ReadCheckLog reads some lines of a status check's log.
func (a Clients) ReadCheckLog(checkID string, firstLine, count int32) (*pullrequestv1.ReadCheckLogResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*pullrequestv1.ReadCheckLogResponse, error) {
		resp, err := a.PullRequests.ReadCheckLog(ctx, connect.NewRequest(&pullrequestv1.ReadCheckLogRequest{
			CheckId:   checkID,
			FirstLine: firstLine,
			Count:     count,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to read the log of check %q: %w", checkID, err)
		}

		return resp.Msg, nil
	})
}

// GetLastScreen reads the screen an agent's pane last saved.
func (a Clients) GetLastScreen(agentID string) (*agentv1.GetLastScreenResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*agentv1.GetLastScreenResponse, error) {
		resp, err := a.Agents.GetLastScreen(ctx, connect.NewRequest(&agentv1.GetLastScreenRequest{AgentId: agentID}))
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, ErrNoSavedScreen
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read the last screen of agent %q: %w", agentID, err)
		}

		return resp.Msg, nil
	})
}

// GetDocument reads a document.
func (a Clients) GetDocument(id string) (*agentv1.Document, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*agentv1.Document, error) {
		resp, err := a.Agents.GetDocument(ctx, connect.NewRequest(&agentv1.GetDocumentRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to read document %q: %w", id, err)
		}

		return resp.Msg.GetDocument(), nil
	})
}

// GetDocumentVersion reads one version of a document.
func (a Clients) GetDocumentVersion(id string, version int32) (*agentv1.Document, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*agentv1.Document, error) {
		resp, err := a.Agents.GetDocument(ctx, connect.NewRequest(&agentv1.GetDocumentRequest{Id: id, Version: &version}))
		if err != nil {
			return nil, fmt.Errorf("failed to read version %d of document %q: %w", version, id, err)
		}

		return resp.Msg.GetDocument(), nil
	})
}

// ListDocumentVersions lists every version of a document.
func (a Clients) ListDocumentVersions(id string) ([]*agentv1.DocumentVersion, error) {
	return Call(RequestTimeout, func(ctx context.Context) ([]*agentv1.DocumentVersion, error) {
		resp, err := a.Agents.ListDocumentVersions(ctx, connect.NewRequest(&agentv1.ListDocumentVersionsRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the versions of document %q: %w", id, err)
		}

		return resp.Msg.GetVersions(), nil
	})
}

// CreateDocument writes a new document.
func (a Clients) CreateDocument(agentID, name, typeName, body string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.WriteDocument(ctx, connect.NewRequest(&agentv1.WriteDocumentRequest{
			AgentId: &agentID,
			Name:    name,
			Type:    typeName,
			Source:  &agentv1.WriteDocumentRequest_Body{Body: body},
		}))
	})
}

// WriteDocumentParams is a write over a document's version, under the name and type it keeps.
type WriteDocumentParams struct {
	ID      string
	Version int32
	Name    string
	Type    string
	Body    string
}

// WriteDocument writes over a document.
func (a Clients) WriteDocument(params WriteDocumentParams) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.WriteDocument(ctx, connect.NewRequest(&agentv1.WriteDocumentRequest{
			Id:      &params.ID,
			Version: &params.Version,
			Name:    params.Name,
			Type:    params.Type,
			Source:  &agentv1.WriteDocumentRequest_Body{Body: params.Body},
		}))
	})
}

// CreateDocumentType defines a document type.
func (a Clients) CreateDocumentType(name, typeContext string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.WriteDocumentType(ctx, connect.NewRequest(&agentv1.WriteDocumentTypeRequest{
			Name:    name,
			Context: typeContext,
		}))
	})
}

// WriteDocumentTypeParams is a write over a document type's version.
type WriteDocumentTypeParams struct {
	ID      string
	Version int32
	Name    string
	Context string
}

// WriteDocumentType rewrites a document type.
func (a Clients) WriteDocumentType(params WriteDocumentTypeParams) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.WriteDocumentType(ctx, connect.NewRequest(&agentv1.WriteDocumentTypeRequest{
			Id:      &params.ID,
			Version: &params.Version,
			Name:    params.Name,
			Context: params.Context,
		}))
	})
}

// DeleteDocumentType deletes a document type.
func (a Clients) DeleteDocumentType(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Agents.DeleteDocumentType(ctx, connect.NewRequest(&agentv1.DeleteDocumentTypeRequest{Id: id}))
	})
}

// ReadAgentLogs reads some lines of agents' logs.
func (a Clients) ReadAgentLogs(req *logv1.ReadAgentLogsRequest) (*logv1.ReadAgentLogsResponse, error) {
	return Call(RequestTimeout, func(ctx context.Context) (*logv1.ReadAgentLogsResponse, error) {
		resp, err := a.Logs.ReadAgentLogs(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, fmt.Errorf("failed to read the %s logs of agents %v: %w", req.GetSource(), req.GetAgentIds(), err)
		}

		return resp.Msg, nil
	})
}

// ListAgentLogSources lists the logs an agent writes.
func (a Clients) ListAgentLogSources(agentID string) ([]*logv1.LogSource, error) {
	return Call(RequestTimeout, func(ctx context.Context) ([]*logv1.LogSource, error) {
		resp, err := a.Logs.ListAgentLogSources(ctx, connect.NewRequest(&logv1.ListAgentLogSourcesRequest{AgentId: agentID}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the log sources of agent %q: %w", agentID, err)
		}

		return resp.Msg.GetSources(), nil
	})
}

// ListTaskLogSources lists the logs a task's agents write.
func (a Clients) ListTaskLogSources(taskID string) ([]*logv1.LogSource, error) {
	return Call(RequestTimeout, func(ctx context.Context) ([]*logv1.LogSource, error) {
		resp, err := a.Logs.ListTaskLogSources(ctx, connect.NewRequest(&logv1.ListTaskLogSourcesRequest{TaskId: taskID}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the log sources of task %q: %w", taskID, err)
		}

		return resp.Msg.GetSources(), nil
	})
}

// WakeLoop asks the server to run a background loop before its next tick.
func (a Clients) WakeLoop(loop loopv1.Loop) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.Loops.WakeLoop(ctx, connect.NewRequest(&loopv1.WakeLoopRequest{Loop: loop}))
	})
}

// PickForkPoint records the commit an orphaned task's own commits sit on.
func (a Clients) PickForkPoint(id, commit string) error {
	_, err := Call(RequestTimeout, func(ctx context.Context) (*agentv1.PickForkPointResponse, error) {
		resp, err := a.Agents.PickForkPoint(ctx, connect.NewRequest(&agentv1.PickForkPointRequest{Task: &agentv1.PickForkPointRequest_TaskId{TaskId: id}, Commit: commit}))
		if err != nil {
			return nil, fmt.Errorf("failed to pick the fork point of task %q: %w", id, err)
		}

		return resp.Msg, nil
	})

	return err
}

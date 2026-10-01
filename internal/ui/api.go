package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"komrad/internal/client"
	"komrad/internal/rpcapi"
	"komrad/internal/ui/panel"
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
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	schedulev1 "komrad/proto/gen/schedule/v1"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"komrad/lib/xslice"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

const requestTimeout = 10 * time.Second

const createAgentTimeout = 11 * time.Minute

type api struct {
	agents       agentv1connect.AgentServiceClient
	workflows    workflowv1connect.WorkflowServiceClient
	schedules    schedulev1connect.ScheduleServiceClient
	hooks        hookv1connect.HookServiceClient
	worktrees    worktreev1connect.WorktreeServiceClient
	logs         logv1connect.LogServiceClient
	forges       forgev1connect.ForgeServiceClient
	pullRequests pullrequestv1connect.PullRequestServiceClient
	loops        loopv1connect.LoopServiceClient
}

// reads is the half of the service a screen reaches without the host.
type reads struct {
	agents agentReads
	logs   logv1connect.LogServiceClient
}

// agentReads is the part of the agent service a screen reads through.
type agentReads interface {
	client.TranscriptGetter
	SearchTranscripts(ctx context.Context, req *connect.Request[agentv1.SearchTranscriptsRequest]) (*connect.Response[agentv1.SearchTranscriptsResponse], error)
	ListReviewThreads(ctx context.Context, req *connect.Request[agentv1.ListReviewThreadsRequest]) (*connect.Response[agentv1.ListReviewThreadsResponse], error)
	OpenReviewRound(ctx context.Context, req *connect.Request[agentv1.OpenReviewRoundRequest]) (*connect.Response[agentv1.OpenReviewRoundResponse], error)
	ListReviewRounds(ctx context.Context, req *connect.Request[agentv1.ListReviewRoundsRequest]) (*connect.Response[agentv1.ListReviewRoundsResponse], error)
	ListReviews(ctx context.Context, req *connect.Request[agentv1.ListReviewsRequest]) (*connect.Response[agentv1.ListReviewsResponse], error)
	ListReviewViewedFiles(ctx context.Context, req *connect.Request[agentv1.ListReviewViewedFilesRequest]) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error)
}

// reads is the half of the service a screen reaches without the host.
func (a api) reads() reads {
	return reads{agents: a.agents, logs: a.logs}
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

func mutateAwaitingHooks(fn func(context.Context) (any, error)) error {
	_, err := call(rpcapi.HookCallTimeout, fn)

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

func (a api) adopt(params adoptParams) (*agentv1.AdoptResponse, error) {
	return call(createAgentTimeout, func(ctx context.Context) (*agentv1.AdoptResponse, error) {
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

		return resp.Msg, nil
	})
}

func (a api) archiveAgents(ids []string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	})
}

// mergePullRequest asks the forge to merge the pull request at the address.
func (a api) mergePullRequest(address string) error {
	_, err := call(mergeTimeout, func(ctx context.Context) (any, error) {
		return a.forges.MergeForgePullRequest(ctx, connect.NewRequest(&forgev1.MergeForgePullRequestRequest{Url: address}))
	})

	return err
}

// setPullRequestAutoMerge asks the forge to merge the pull request at the address on its own once
// its checks and reviews pass, or to stop waiting to, refusing when its head has moved past
// headSHA.
func (a api) setPullRequestAutoMerge(address, headSHA string, on bool) error {
	_, err := call(autoMergeTimeout, func(ctx context.Context) (any, error) {
		return a.forges.SetForgePullRequestAutoMerge(ctx, connect.NewRequest(&forgev1.SetForgePullRequestAutoMergeRequest{
			Url:     address,
			HeadSha: headSHA,
			On:      on,
		}))
	})

	return err
}

// setPullRequestDraft asks the forge to mark the pull request at the address a draft or ready.
func (a api) setPullRequestDraft(address string, draft bool) error {
	_, err := call(draftTimeout, func(ctx context.Context) (any, error) {
		return a.forges.SetForgePullRequestDraft(ctx, connect.NewRequest(&forgev1.SetForgePullRequestDraftRequest{Url: address, Draft: draft}))
	})

	return err
}

func (a api) setHidden(agent panel.Agent, hidden bool) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: agent.ID, Hidden: hidden}))
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

func (a api) restoreAgents(ids []string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
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
	return call(rpcapi.HookCallTimeout, func(ctx context.Context) (*agentv1.Agent, error) {
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

func (a api) planPush(id string) (*agentv1.PlanPushResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.PlanPushResponse, error) {
		resp, err := a.agents.PlanPush(ctx, connect.NewRequest(&agentv1.PlanPushRequest{AgentId: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to work out what a push of agent %q would do: %w", id, err)
		}

		return resp.Msg, nil
	})
}

func (a api) pushWorktree(id string) (*agentv1.PushWorktreeResponse, error) {
	return call(pushTimeout, func(ctx context.Context) (*agentv1.PushWorktreeResponse, error) {
		resp, err := a.agents.PushWorktree(ctx, connect.NewRequest(&agentv1.PushWorktreeRequest{AgentId: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to push the branch of agent %q: %w", id, err)
		}

		return resp.Msg, nil
	})
}

func (a api) abortSequence(id string) (*agentv1.AbortSequenceResponse, error) {
	return call(rebaseTimeout, func(ctx context.Context) (*agentv1.AbortSequenceResponse, error) {
		resp, err := a.agents.AbortSequence(ctx, connect.NewRequest(&agentv1.AbortSequenceRequest{AgentId: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to abort what the worktree of agent %q has in progress: %w", id, err)
		}

		return resp.Msg, nil
	})
}

func (a api) deleteAgents(ids []string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: ids}))
	})
}

func (a api) createSchedule(cfg panel.ScheduleConfig) error {
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

		return a.schedules.CreateSchedule(ctx, connect.NewRequest(req))
	})
}

func (a api) updateSchedule(id string, cfg panel.ScheduleConfig) error {
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

		return a.schedules.UpdateSchedule(ctx, connect.NewRequest(req))
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

func (a api) restoreSchedule(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.schedules.RestoreSchedule(ctx, connect.NewRequest(&schedulev1.RestoreScheduleRequest{Id: id}))
	})
}

func (a api) deleteSchedule(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.schedules.DeleteSchedule(ctx, connect.NewRequest(&schedulev1.DeleteScheduleRequest{Id: id}))
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

func promptsToProto(prompts []panel.Prompt) []*workflowv1.WorkflowPrompt {
	return xslice.Map(prompts, func(prompt panel.Prompt) *workflowv1.WorkflowPrompt {
		return &workflowv1.WorkflowPrompt{Notice: prompt.Notice, Prompt: prompt.Prompt}
	})
}

func (a api) createWorkflow(cfg panel.WorkflowConfig) (*workflowv1.Workflow, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "workflow"
	}

	return call(requestTimeout, func(ctx context.Context) (*workflowv1.Workflow, error) {
		resp, err := a.workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
			Name:            name,
			Context:         cfg.Context,
			Harness:         cfg.Harness,
			Cwd:             cfg.Cwd,
			Flags:           cfg.Flags,
			ReviewerHarness: cfg.ReviewerHarness,
			ReviewerFlags:   cfg.ReviewerFlags,
			Worktree:        worktreeConfigToProto(cfg.Worktree),
			Hooks:           hooksToProto(cfg.Hooks),
			Prompts:         promptsToProto(cfg.Prompts),
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
			Prompts:         promptsToProto(w.Prompts),
		}))
	})
}

func (a api) archiveWorkflow(id string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
		return a.workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: id}))
	})
}

func (a api) restoreWorkflow(id string) error {
	return mutateAwaitingHooks(func(ctx context.Context) (any, error) {
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

func (a api) listDocuments(agentID string) ([]*agentv1.Document, error) {
	return call(requestTimeout, func(ctx context.Context) ([]*agentv1.Document, error) {
		resp, err := a.agents.ListDocuments(ctx, connect.NewRequest(&agentv1.ListDocumentsRequest{AgentId: &agentID}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the documents of agent %q: %w", agentID, err)
		}

		return resp.Msg.GetDocuments(), nil
	})
}

func (a api) getLastScreen(agentID string) (*agentv1.GetLastScreenResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.GetLastScreenResponse, error) {
		resp, err := a.agents.GetLastScreen(ctx, connect.NewRequest(&agentv1.GetLastScreenRequest{AgentId: agentID}))
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, errNoSavedScreen
		}

		if err != nil {
			return nil, fmt.Errorf("failed to read the last screen of agent %q: %w", agentID, err)
		}

		return resp.Msg, nil
	})
}

func (a api) getDocument(id string) (*agentv1.Document, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.Document, error) {
		resp, err := a.agents.GetDocument(ctx, connect.NewRequest(&agentv1.GetDocumentRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to read document %q: %w", id, err)
		}

		return resp.Msg.GetDocument(), nil
	})
}

func (a api) getDocumentVersion(id string, version int32) (*agentv1.Document, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.Document, error) {
		resp, err := a.agents.GetDocument(ctx, connect.NewRequest(&agentv1.GetDocumentRequest{Id: id, Version: &version}))
		if err != nil {
			return nil, fmt.Errorf("failed to read version %d of document %q: %w", version, id, err)
		}

		return resp.Msg.GetDocument(), nil
	})
}

func (a api) listDocumentVersions(id string) ([]*agentv1.DocumentVersion, error) {
	return call(requestTimeout, func(ctx context.Context) ([]*agentv1.DocumentVersion, error) {
		resp, err := a.agents.ListDocumentVersions(ctx, connect.NewRequest(&agentv1.ListDocumentVersionsRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the versions of document %q: %w", id, err)
		}

		return resp.Msg.GetVersions(), nil
	})
}

func (a api) createDocument(agentID, name, typeName, body string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.WriteDocument(ctx, connect.NewRequest(&agentv1.WriteDocumentRequest{
			AgentId: &agentID,
			Name:    name,
			Type:    typeName,
			Source:  &agentv1.WriteDocumentRequest_Body{Body: body},
		}))
	})
}

func (a api) writeDocument(item documentItem, version int32, body string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.WriteDocument(ctx, connect.NewRequest(&agentv1.WriteDocumentRequest{
			Id:      &item.id,
			Version: &version,
			Name:    item.name,
			Type:    item.typeName,
			Source:  &agentv1.WriteDocumentRequest_Body{Body: body},
		}))
	})
}

func (a api) getDocumentTypeVersion(name string, version int32) (*agentv1.DocumentType, error) {
	return call(requestTimeout, func(ctx context.Context) (*agentv1.DocumentType, error) {
		resp, err := a.agents.GetDocumentType(ctx, connect.NewRequest(&agentv1.GetDocumentTypeRequest{Name: name, Version: &version}))
		if err != nil {
			return nil, fmt.Errorf("failed to read version %d of document type %q: %w", version, name, err)
		}

		return resp.Msg.GetDocumentType(), nil
	})
}

func (a api) listDocumentTypeVersions(id string) ([]*agentv1.DocumentTypeVersion, error) {
	return call(requestTimeout, func(ctx context.Context) ([]*agentv1.DocumentTypeVersion, error) {
		resp, err := a.agents.ListDocumentTypeVersions(ctx, connect.NewRequest(&agentv1.ListDocumentTypeVersionsRequest{Id: id}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the versions of document type %q: %w", id, err)
		}

		return resp.Msg.GetVersions(), nil
	})
}

func (a api) createDocumentType(name, typeContext string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.WriteDocumentType(ctx, connect.NewRequest(&agentv1.WriteDocumentTypeRequest{
			Name:    name,
			Context: typeContext,
		}))
	})
}

func (a api) writeDocumentType(item documentItem, version int32, name, typeContext string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.WriteDocumentType(ctx, connect.NewRequest(&agentv1.WriteDocumentTypeRequest{
			Id:      &item.id,
			Version: &version,
			Name:    name,
			Context: typeContext,
		}))
	})
}

func (a api) deleteDocumentType(id string) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.agents.DeleteDocumentType(ctx, connect.NewRequest(&agentv1.DeleteDocumentTypeRequest{Id: id}))
	})
}

func (a reads) readAgentLogs(req *logv1.ReadAgentLogsRequest) (*logv1.ReadAgentLogsResponse, error) {
	return call(requestTimeout, func(ctx context.Context) (*logv1.ReadAgentLogsResponse, error) {
		resp, err := a.logs.ReadAgentLogs(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, fmt.Errorf("failed to read the %s logs of agents %v: %w", req.GetSource(), req.GetAgentIds(), err)
		}

		return resp.Msg, nil
	})
}

func (a reads) listAgentLogSources(agentID string) ([]*logv1.LogSource, error) {
	return call(requestTimeout, func(ctx context.Context) ([]*logv1.LogSource, error) {
		resp, err := a.logs.ListAgentLogSources(ctx, connect.NewRequest(&logv1.ListAgentLogSourcesRequest{AgentId: agentID}))
		if err != nil {
			return nil, fmt.Errorf("failed to list the log sources of agent %q: %w", agentID, err)
		}

		return resp.Msg.GetSources(), nil
	})
}

// wakeLoop asks the service to run a background loop before its next tick.
func (a api) wakeLoop(loop loopv1.Loop) error {
	return mutate(func(ctx context.Context) (any, error) {
		return a.loops.WakeLoop(ctx, connect.NewRequest(&loopv1.WakeLoopRequest{Loop: loop}))
	})
}

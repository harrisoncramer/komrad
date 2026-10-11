package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/scripts"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
)

var ErrWorkflowHasSchedules = fail.Precondition("workflow still has schedules")

var ErrWorktreeNeedsCwd = fail.InvalidArgument("a worktree-enabled workflow needs a cwd naming the repo")

var ErrWorktreeCwdNotPrimaryCheckout = fail.InvalidArgument("a worktree-enabled workflow's cwd must be a primary checkout, not a linked worktree, a bare repo, or a submodule")

var ErrUnknownHookEvent = fail.InvalidArgument("a hook must name a moment komrad fires")

var ErrUnknownHookScript = fail.InvalidArgument("a hook must name a script in the hooks directory")

var ErrWorktreeNeedsBase = fail.InvalidArgument("a workflow's base branch cannot be blank")

type workflowAgentDeleter interface {
	DeleteAgents(ctx context.Context, ids []string) error
}

type workflowHookFinder interface {
	FindHook(name string) (Script, error)
}

type NewWorkflowServiceParams struct {
	Workflows      *workflowstore.WorkflowStore
	Agents         *agentstore.AgentStore
	AgentDeleter   workflowAgentDeleter
	EmptyWorkflows *EmptyWorkflowDeleter
	Schedules      *schedulestore.ScheduleStore
	Lifecycle      *LifecycleService
	Tx             txRunner
	Hooks          workflowHookFinder
}

type WorkflowService struct {
	workflows      *workflowstore.WorkflowStore
	agents         *agentstore.AgentStore
	agentDeleter   workflowAgentDeleter
	emptyWorkflows *EmptyWorkflowDeleter
	schedules      *schedulestore.ScheduleStore
	lifecycle      *LifecycleService
	tx             txRunner
	hooks          workflowHookFinder
}

// NewWorkflowService builds the service from its dependencies.
func NewWorkflowService(params NewWorkflowServiceParams) *WorkflowService {
	return &WorkflowService{
		workflows:      params.Workflows,
		agents:         params.Agents,
		agentDeleter:   params.AgentDeleter,
		emptyWorkflows: params.EmptyWorkflows,
		schedules:      params.Schedules,
		lifecycle:      params.Lifecycle,
		tx:             params.Tx,
		hooks:          params.Hooks,
	}
}

// GetWorkflow returns a workflow by id, or nil when none has that id.
func (w *WorkflowService) GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	found, err := w.workflows.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	return found, nil
}

type ListWorkflowsParams struct {
	Limit    int64
	Cursor   *workflowstore.Cursor
	Archived bool
}

// ListWorkflows returns one page of the live or the archived workflows, by name.
func (w *WorkflowService) ListWorkflows(ctx context.Context, params ListWorkflowsParams) ([]workflowstore.Workflow, error) {
	listed, err := w.workflows.ListPage(ctx, params.Limit, params.Cursor, params.Archived)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	return listed, nil
}

// NewWorkflow creates a workflow, defaulting a blank name so the list always has a label and a
// blank base branch to what the cwd's clone names.
func (w *WorkflowService) NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error) {
	cfg.Normalize()
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}
	if cfg.Worktree.BaseBranch == "" {
		cfg.Worktree.BaseBranch = git.DefaultBase(ctx, cfg.Cwd)
	}

	if err := w.normalizeConfig(&cfg); err != nil {
		return nil, err
	}

	// The workflow, its blueprint, and its provisioner config are three rows, so a failure partway
	// through must not leave any of them half-written.
	var created *workflowstore.Workflow
	err := w.tx.InTx(ctx, func(db store.DBTX) error {
		var txErr error
		created, txErr = workflowstore.NewWorkflowStore(db).Create(ctx, cfg)

		return txErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return created, nil
}

// UpdateWorkflow replaces a workflow's editable fields, keeping the name it already has when a
// blank one is given.
func (w *WorkflowService) UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error {
	existing, err := w.GetWorkflow(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return agenterr.ErrWorkflowNotFound.Saying("no workflow with id %q", id)
	}
	if err != nil {
		return err
	}

	cfg.Normalize()
	if cfg.Name == "" {
		cfg.Name = existing.Name
	}
	if cfg.Worktree.BaseBranch == "" {
		return ErrWorktreeNeedsBase
	}

	if err := w.normalizeConfig(&cfg); err != nil {
		return err
	}

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		return workflowstore.NewWorkflowStore(db).Update(ctx, id, cfg)
	})
	if err != nil {
		return fmt.Errorf("failed to update workflow %q: %w", id, err)
	}

	return nil
}

func (w *WorkflowService) normalizeConfig(cfg *workflowstore.Config) error {
	if err := cfg.Worktree.ValidatePaths(); err != nil {
		return err
	}

	hooks, err := w.normalizeHooks(cfg.Hooks)
	if err != nil {
		return err
	}
	cfg.Hooks = hooks

	if err := cfg.NormalizeEventPrompts(); err != nil {
		return err
	}

	if cfg.Worktree.Enabled {
		return requireWorktreeRepo(*cfg)
	}

	return nil
}

// normalizeHooks trims what a caller sent, drops the blank entries, and refuses a moment komrad
// does not fire or a script the hooks directory does not hold.
func (w *WorkflowService) normalizeHooks(hooks []workflowstore.Hook) ([]workflowstore.Hook, error) {
	kept := make([]workflowstore.Hook, 0, len(hooks))
	for _, hook := range hooks {
		hook.Event = strings.TrimSpace(hook.Event)
		hook.Script = strings.TrimSpace(hook.Script)
		if hook.Script == "" {
			continue
		}
		if !slices.Contains(workflowstore.HookEvents(), hook.Event) {
			return nil, fmt.Errorf("%w: %q is not one of %s", ErrUnknownHookEvent, hook.Event, strings.Join(workflowstore.HookEvents(), ", "))
		}
		if _, err := w.hooks.FindHook(hook.Script); errors.Is(err, scripts.ErrScriptNotFound) {
			return nil, fmt.Errorf("%w: %s: %s", ErrUnknownHookScript, hook.Event, err.Error())
		} else if err != nil {
			return nil, fmt.Errorf("failed to find hook %q: %w", hook.Script, err)
		}

		kept = append(kept, hook)
	}

	return kept, nil
}

// requireWorktreeRepo holds a workflow that provisions worktrees to a cwd it can cut them from.
func requireWorktreeRepo(cfg workflowstore.Config) error {
	if cfg.Cwd == "" {
		return ErrWorktreeNeedsCwd
	}

	return requirePrimaryCheckout(cfg.Cwd)
}

func requirePrimaryCheckout(cwd string) error {
	info, err := os.Stat(filepath.Join(cwd, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %q", ErrWorktreeCwdNotPrimaryCheckout, cwd)
	}

	return nil
}

// ArchiveWorkflow archives a workflow and every agent inside it.
func (w *WorkflowService) ArchiveWorkflow(ctx context.Context, id string) error {
	existing, err := w.GetWorkflow(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return agenterr.ErrWorkflowNotFound.Saying("no workflow with id %q", id)
	}
	if err != nil {
		return err
	}

	scheduled, err := w.schedules.CountLive(ctx, id)
	if err != nil {
		return err
	}
	if scheduled > 0 {
		// Archiving would strand a live schedule or kill it silently, so it has to be turned off
		// first.
		return fmt.Errorf("%w: %q has %d", ErrWorkflowHasSchedules, existing.Name, scheduled)
	}

	inside, err := w.agents.ListLiveInWorkflow(ctx, id)
	if err != nil {
		return err
	}

	if err := w.lifecycle.archiveWorkflow(ctx, id, inside); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", existing.Name, err)
	}

	return nil
}

// RestoreWorkflow brings an archived workflow back with the agents its archive took, landing them
// dormant so a folder of twelve does not start twelve CLIs.
func (w *WorkflowService) RestoreWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	existing, err := w.workflows.GetIncludingArchived(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, agenterr.ErrWorkflowNotFound.Saying("no archived workflow with id %q", id)
	}
	if err != nil {
		return nil, err
	}
	if existing.ArchivedAt == nil {
		return nil, agenterr.ErrWorkflowNotFound.Saying("no archived workflow with id %q", id)
	}

	archived, err := w.agents.ListRestorableInWorkflow(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to list the archived agents of workflow %q: %w", existing.Name, err)
	}

	if err := w.lifecycle.restoreWorkflow(ctx, id, archived); err != nil {
		return nil, fmt.Errorf("failed to restore workflow %q: %w", existing.Name, err)
	}

	return w.workflows.Get(ctx, id)
}

// DeleteWorkflow permanently removes an archived workflow along with whatever archived agents
// and schedules are still inside it.
func (w *WorkflowService) DeleteWorkflow(ctx context.Context, id string) error {
	existing, err := w.workflows.GetIncludingArchived(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return agenterr.ErrWorkflowNotFound.Saying("no archived workflow with id %q", id)
	}
	if err != nil {
		return err
	}
	if existing.ArchivedAt == nil {
		return agenterr.ErrWorkflowNotFound.Saying("no archived workflow with id %q", id)
	}

	inside, err := w.agents.ListArchivedInWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if len(inside) == 0 {
		// No agent delete runs to check an empty workflow, so this is the one case that removes
		// the row here.
		return w.emptyWorkflows.deleteWithSchedules(ctx, existing)
	}

	// Deleting an agent checks the workflow it leaves behind for itself, and leaves behind any
	// agent whose cleanup is still running or has failed.
	return w.agentDeleter.DeleteAgents(ctx, agentstore.IDs(inside))
}

type claimRoomToRestoreParams struct {
	agents   *agentstore.AgentStore
	usage    config.Usage
	archived []agentstore.Agent
}

func claimRoomToRestore(ctx context.Context, params claimRoomToRestoreParams) error {
	if err := claimRoomForAgents(ctx, params.agents, params.usage.MaxAgents, len(params.archived)); err != nil {
		return err
	}
	if err := claimRoomForRestoredChildren(ctx, params.agents, params.usage.Implementer.MaxChildren, params.archived); err != nil {
		return err
	}
	if err := claimRoomForRestoredSpawned(ctx, params.agents, params.usage.Orchestrator.MaxChildren, params.archived); err != nil {
		return err
	}

	return requireNamesFree(ctx, params.agents, params.archived)
}

func requireNamesFree(ctx context.Context, agents *agentstore.AgentStore, archived []agentstore.Agent) error {
	returning := make(map[agentName]int, len(archived))
	for _, agent := range archived {
		returning[agentName{workflowID: agent.WorkflowID, name: agent.Name}]++
	}

	var clashing []string
	checked := make(map[agentName]bool, len(archived))

	for _, agent := range archived {
		claimed := agentName{workflowID: agent.WorkflowID, name: agent.Name}
		if checked[claimed] {
			continue
		}
		checked[claimed] = true

		if returning[claimed] > 1 {
			clashing = append(clashing, agent.Name)

			continue
		}

		taken, err := agents.ExistsActiveName(ctx, agent.WorkflowID, agent.Name)
		if err != nil {
			return fmt.Errorf("failed to check the name of agent %q: %w", agent.Name, err)
		}
		if taken {
			clashing = append(clashing, agent.Name)
		}
	}
	if len(clashing) > 0 {
		return fmt.Errorf("%w: rename whatever holds %s before restoring", store.ErrNameTaken, strings.Join(clashing, ", "))
	}

	return nil
}

type agentName struct {
	workflowID string
	name       string
}

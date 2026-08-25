package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

const (
	worktreeSetupAction   = "_worktree_setup"
	worktreeCleanupAction = "_worktree_cleanup"
)

var ErrWorkflowHasSchedules = errors.New("workflow still has schedules")

var ErrWorktreeNeedsCwd = errors.New("a worktree-enabled workflow needs a cwd naming the repo")

var ErrWorktreeCwdNotPrimaryCheckout = errors.New("a worktree-enabled workflow's cwd must be a primary checkout, not a linked worktree, a bare repo, or a submodule")

var ErrWorktreeOwnsActions = errors.New("a worktree-enabled workflow points setup_action and cleanup_action at the built-in pair itself, so both must be blank")

var ErrWorktreeActionsReserved = errors.New("setup_action and cleanup_action may not name the built-in worktree pair unless worktree.enabled is set")

var ErrWorktreeUnsafePath = errors.New("a worktree copy or symlink path must be a relative path inside the repo")

type workflowReadWriter interface {
	List(ctx context.Context) ([]workflowstore.Workflow, error)
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
	Delete(ctx context.Context, id string) error
}

type scheduleCounter interface {
	CountLive(ctx context.Context, workflowID string) (int64, error)
}

type workflowAgentLister interface {
	ListLiveInWorkflow(ctx context.Context, workflowID string) ([]agentstore.Agent, error)
	ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]agentstore.Agent, error)
}

type workflowAgentDeleter interface {
	DeleteAgents(ctx context.Context, ids []string) error
}

type WorkflowService struct {
	store        workflowReadWriter
	agents       workflowAgentLister
	agentDeleter workflowAgentDeleter
	schedules    scheduleCounter
	tmux         paneKiller
	tx           txRunner

	maxAgents int
	notifier  parentNotifier
}

type WorkflowParams struct {
	Workflows    workflowReadWriter
	Agents       workflowAgentLister
	AgentDeleter workflowAgentDeleter
	Schedules    scheduleCounter
	Tmux         paneKiller
	Tx           txRunner
	MaxAgents    int
	Notifier     parentNotifier
}

// NewWorkflowService builds the service from its dependencies.
func NewWorkflowService(p WorkflowParams) *WorkflowService {
	return &WorkflowService{
		store:        p.Workflows,
		agents:       p.Agents,
		agentDeleter: p.AgentDeleter,
		schedules:    p.Schedules,
		tmux:         p.Tmux,
		tx:           p.Tx,
		maxAgents:    p.MaxAgents,
		notifier:     p.Notifier,
	}
}

// GetWorkflow returns a workflow by id, or nil when none has that id.
func (w *WorkflowService) GetWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	found, err := w.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	return found, nil
}

// NewWorkflow creates a workflow, defaulting a blank name so the list always has
// a label to render. The cwd is home-expanded so the stored blueprint matches
// what an agent launch would use.
//
// The write spans three rows, the workflow itself plus its blueprint and its action
// config, so it runs inside a transaction. A failure partway through must not leave a
// workflow_configs or workflow_action_configs row with nothing pointing at it, or worse,
// a workflow row half-built.
func (w *WorkflowService) NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = config.ExpandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)
	cfg.SetupAction = strings.TrimSpace(cfg.SetupAction)
	cfg.CleanupAction = strings.TrimSpace(cfg.CleanupAction)
	normalizeWorktree(&cfg)

	if err := validateWorktreePaths(cfg); err != nil {
		return nil, err
	}

	if cfg.Worktree.Enabled {
		if err := setWorktreeActions(&cfg); err != nil {
			return nil, err
		}
	} else if namesReservedWorktreeAction(cfg) {
		return nil, ErrWorktreeActionsReserved
	}

	var created *workflowstore.Workflow
	err := w.tx.InTx(ctx, func(db store.DBTX) error {
		var txErr error
		created, txErr = workflowstore.New(db).Create(ctx, cfg)

		return txErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return created, nil
}

// UpdateWorkflow replaces a workflow's editable fields. A blank name falls back
// to the one it already has, so the list never renders an unlabelled bucket. The write
// touches the workflow row, its blueprint, and its action config together, so it runs
// inside a transaction the same way NewWorkflow's does.
//
// Turning worktree support off clears the setup and cleanup actions only when they still
// name the built-in pair, leaving alone anything a caller pointed at something else since.
func (w *WorkflowService) UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error {
	existing, err := w.GetWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
	}

	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = existing.Name
	}

	cfg.Cwd = config.ExpandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)
	cfg.SetupAction = strings.TrimSpace(cfg.SetupAction)
	cfg.CleanupAction = strings.TrimSpace(cfg.CleanupAction)
	normalizeWorktree(&cfg)

	if err := validateWorktreePaths(cfg); err != nil {
		return err
	}

	switch {
	case cfg.Worktree.Enabled:
		if err := setWorktreeActions(&cfg); err != nil {
			return err
		}
	case existing.Worktree.Enabled:
		if cfg.SetupAction == worktreeSetupAction {
			cfg.SetupAction = ""
		}
		if cfg.CleanupAction == worktreeCleanupAction {
			cfg.CleanupAction = ""
		}
	case namesReservedWorktreeAction(cfg):
		return ErrWorktreeActionsReserved
	}

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		return workflowstore.New(db).Update(ctx, id, cfg)
	})
	if err != nil {
		return fmt.Errorf("failed to update workflow %q: %w", id, err)
	}

	return nil
}

func namesReservedWorktreeAction(cfg workflowstore.Config) bool {
	return cfg.SetupAction == worktreeSetupAction || cfg.CleanupAction == worktreeCleanupAction
}

func normalizeWorktree(cfg *workflowstore.Config) {
	cfg.Worktree.BaseBranch = strings.TrimSpace(cfg.Worktree.BaseBranch)
	cfg.Worktree.CopyPaths = trimPaths(cfg.Worktree.CopyPaths)
	cfg.Worktree.SymlinkPaths = trimPaths(cfg.Worktree.SymlinkPaths)
}

func trimPaths(paths []string) []string {
	if paths == nil {
		return nil
	}

	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}

	return kept
}

func validateWorktreePaths(cfg workflowstore.Config) error {
	for _, list := range [][]string{cfg.Worktree.CopyPaths, cfg.Worktree.SymlinkPaths} {
		for _, path := range list {
			if err := validateWorktreePath(path); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateWorktreePath(path string) error {
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("%w: %q holds a line break, which is what separates the entries", ErrWorktreeUnsafePath, path)
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "~") {
		return fmt.Errorf("%w: %q is not relative to the repo", ErrWorktreeUnsafePath, path)
	}

	segments := strings.Split(path, "/")
	for _, segment := range segments {
		if segment == ".." {
			return fmt.Errorf("%w: %q climbs out of the repo", ErrWorktreeUnsafePath, path)
		}
	}

	if strings.EqualFold(segments[0], ".git") {
		return fmt.Errorf("%w: %q is the worktree's own link to the repo", ErrWorktreeUnsafePath, path)
	}

	return nil
}

func setWorktreeActions(cfg *workflowstore.Config) error {
	if cfg.Cwd == "" {
		return ErrWorktreeNeedsCwd
	}
	if cfg.SetupAction != "" && cfg.SetupAction != worktreeSetupAction {
		return ErrWorktreeOwnsActions
	}
	if cfg.CleanupAction != "" && cfg.CleanupAction != worktreeCleanupAction {
		return ErrWorktreeOwnsActions
	}
	if err := requirePrimaryCheckout(cfg.Cwd); err != nil {
		return err
	}

	cfg.SetupAction = worktreeSetupAction
	cfg.CleanupAction = worktreeCleanupAction

	return nil
}

func requirePrimaryCheckout(cwd string) error {
	info, err := os.Stat(filepath.Join(cwd, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %q", ErrWorktreeCwdNotPrimaryCheckout, cwd)
	}

	return nil
}

// ArchiveWorkflow archives a workflow and the agents inside it, since every agent belongs to
// a workflow and none can be left behind. Panes are killed before the transaction commits, so
// a failed commit leaves rows the reconciler can still tidy rather than hiding a live CLI for
// good. A workflow with live schedules is refused, since archiving it would strand or
// silently kill them; they must be turned off first. The workflow and agent rows span
// different domains, so both are archived in one transaction to avoid a half-applied archive.
func (w *WorkflowService) ArchiveWorkflow(ctx context.Context, id string) error {
	existing, err := w.GetWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
	}

	scheduled, err := w.schedules.CountLive(ctx, id)
	if err != nil {
		return err
	}
	if scheduled > 0 {
		return fmt.Errorf("%w: %q has %d", ErrWorkflowHasSchedules, existing.Name, scheduled)
	}

	inside, err := w.agents.ListLiveInWorkflow(ctx, id)
	if err != nil {
		return err
	}
	killOwnedPanes(ctx, w.tmux, inside)

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		if err := workflowstore.New(db).Archive(ctx, id); err != nil {
			return err
		}

		return agentstore.New(db).ArchiveWorkflowAgents(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	notifyParents(ctx, w.notifier, inside, "archived")

	return nil
}

// RestoreWorkflow brings an archived workflow back with every agent inside it, landing them
// dormant so a folder of twelve does not start twelve CLIs. It is all or nothing, under the
// agent ceiling and against the names live agents hold, since a partial restore would read
// as whole; a refusal names what is in the way so the user can clear it and retry.
func (w *WorkflowService) RestoreWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	existing, err := w.store.GetIncludingArchived(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.ArchivedAt == nil {
		return nil, fmt.Errorf("%w: %q is not archived", ErrWorkflowNotFound, id)
	}

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		agents := agentstore.New(db)

		archived, err := agents.ListArchivedInWorkflow(ctx, id)
		if err != nil {
			return err
		}
		if err := claimRoomForAgents(ctx, agents, w.maxAgents, len(archived)); err != nil {
			return err
		}
		if err := requireNamesFree(ctx, agents, archived); err != nil {
			return err
		}

		if err := workflowstore.New(db).Restore(ctx, id); err != nil {
			return err
		}

		return agents.RestoreWorkflowAgents(ctx, id)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to restore workflow %q: %w", existing.Name, err)
	}

	return w.store.Get(ctx, id)
}

// DeleteWorkflow permanently removes an archived workflow along with whatever archived agents
// are still inside it. Archiving a workflow archives every agent in it too, so there is never
// a live agent to refuse here. The cascade goes through DeleteAgents rather than a raw store
// delete, so an agent whose cleanup action is still running, or whose cleanup failed, is left
// behind exactly as it would be from a direct delete, and the workflow row goes with it: a
// delete that actually removes an agent checks the workflow it leaves behind for itself,
// synchronously or from a cleanup action's own completion, so there is nothing left to check
// here once agents are cascaded to. A workflow with nothing left to cascade is the exception,
// since no delete runs to check it, so that case is still checked directly.
func (w *WorkflowService) DeleteWorkflow(ctx context.Context, id string) error {
	existing, err := w.store.GetIncludingArchived(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil || existing.ArchivedAt == nil {
		return fmt.Errorf("%w: %q is not archived", ErrWorkflowNotFound, id)
	}

	inside, err := w.agents.ListArchivedInWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if len(inside) == 0 {
		return w.deleteIfEmpty(ctx, existing)
	}

	return w.agentDeleter.DeleteAgents(ctx, agentIDs(inside))
}

// DeleteIfEmpty permanently removes a workflow if it is archived and now holds no agents at
// all, and does nothing otherwise. It is the other half of emptying the archive: a batch
// delete of archived agents can leave one of their workflows with nothing left in it, and
// this is what turns that into the workflow row disappearing too.
func (w *WorkflowService) DeleteIfEmpty(ctx context.Context, id string) error {
	existing, err := w.store.GetIncludingArchived(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil || existing.ArchivedAt == nil {
		return nil
	}

	return w.deleteIfEmpty(ctx, existing)
}

func (w *WorkflowService) deleteIfEmpty(ctx context.Context, existing *workflowstore.Workflow) error {
	remaining, err := w.agents.ListArchivedInWorkflow(ctx, existing.ID)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return nil
	}

	if err := w.store.Delete(ctx, existing.ID); err != nil {
		return fmt.Errorf("failed to delete workflow %q: %w", existing.Name, err)
	}

	return nil
}

func requireNamesFree(ctx context.Context, agents agentManager, archived []agentstore.Agent) error {
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

		taken, err := agents.ActiveNameExists(ctx, agent.WorkflowID, agent.Name)
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

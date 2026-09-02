package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

const (
	worktreeProvisioner   = "_worktree_setup"
	worktreeDeprovisioner = "_worktree_cleanup"
)

var ErrWorkflowHasSchedules = errors.New("workflow still has schedules")

var ErrScheduleHasAgents = errors.New("schedule still has agents outside the workflow being deleted")

var ErrWorktreeNeedsCwd = errors.New("a worktree-enabled workflow needs a cwd naming the repo")

var ErrWorktreeCwdNotPrimaryCheckout = errors.New("a worktree-enabled workflow's cwd must be a primary checkout, not a linked worktree, a bare repo, or a submodule")

var ErrWorktreeUnsafePath = errors.New("a worktree copy or symlink path must be a relative path inside the repo")

var ErrUnknownHookEvent = errors.New("a hook must name a moment weave fires")

type workflowScheduleCounter interface {
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
	store        workflowReader
	agents       workflowAgentLister
	agentDeleter workflowAgentDeleter
	emptied      *EmptyWorkflowDeleter
	schedules    workflowScheduleCounter
	tmux         paneKiller
	tx           txRunner
	maxAgents    int
	maxChildren  int
	notifier     systemMailSender
}

type WorkflowParams struct {
	Workflows    workflowReader
	Agents       workflowAgentLister
	AgentDeleter workflowAgentDeleter
	Emptied      *EmptyWorkflowDeleter
	Schedules    workflowScheduleCounter
	Tmux         paneKiller
	Tx           txRunner
	MaxAgents    int
	MaxChildren  int
	Notifier     systemMailSender
}

// NewWorkflowService builds the service from its dependencies.
func NewWorkflowService(p WorkflowParams) *WorkflowService {
	return &WorkflowService{
		store:        p.Workflows,
		agents:       p.Agents,
		agentDeleter: p.AgentDeleter,
		emptied:      p.Emptied,
		schedules:    p.Schedules,
		tmux:         p.Tmux,
		tx:           p.Tx,
		maxAgents:    p.MaxAgents,
		maxChildren:  p.MaxChildren,
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

// NewWorkflow creates a workflow, defaulting a blank name so the list always has a label to
// render.
func (w *WorkflowService) NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = config.ExpandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)
	normalizeWorktree(&cfg)

	if err := validateWorktreePaths(cfg); err != nil {
		return nil, err
	}

	hooks, err := normalizeHooks(cfg.Hooks)
	if err != nil {
		return nil, err
	}
	cfg.Hooks = hooks

	if cfg.Worktree.Enabled {
		if err := requireWorktreeRepo(cfg); err != nil {
			return nil, err
		}
	}

	// The workflow, its blueprint, and its script config are three rows, so a failure partway
	// through must not leave any of them half-written.
	var created *workflowstore.Workflow
	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		var txErr error
		created, txErr = workflowstore.New(db).Create(ctx, cfg)

		return txErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return created, nil
}

// UpdateWorkflow replaces a workflow's editable fields, keeping the name it already has when a
// blank one is given so the list never renders an unlabelled bucket.
func (w *WorkflowService) UpdateWorkflow(ctx context.Context, id string, cfg workflowstore.Config) error {
	existing, err := w.GetWorkflow(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
	}
	if err != nil {
		return err
	}

	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = existing.Name
	}

	cfg.Cwd = config.ExpandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)
	normalizeWorktree(&cfg)

	if err := validateWorktreePaths(cfg); err != nil {
		return err
	}

	hooks, hooksErr := normalizeHooks(cfg.Hooks)
	if hooksErr != nil {
		return hooksErr
	}
	cfg.Hooks = hooks

	if cfg.Worktree.Enabled {
		if err := requireWorktreeRepo(cfg); err != nil {
			return err
		}
	}

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		return workflowstore.New(db).Update(ctx, id, cfg)
	})
	if err != nil {
		return fmt.Errorf("failed to update workflow %q: %w", id, err)
	}

	return nil
}

// normalizeHooks trims what a caller sent and drops the blanks, since a select with None on it
// sends the empty string, and refuses a moment weave does not fire rather than storing a script
// nothing will ever run.
func normalizeHooks(hooks []workflowstore.Hook) ([]workflowstore.Hook, error) {
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

		kept = append(kept, hook)
	}

	return kept, nil
}

func normalizeWorktree(cfg *workflowstore.Config) {
	cfg.Worktree.BaseBranch = strings.TrimSpace(cfg.Worktree.BaseBranch)
	cfg.Worktree.CopyPaths = trimPaths(cfg.Worktree.CopyPaths)
	cfg.Worktree.SymlinkPaths = trimPaths(cfg.Worktree.SymlinkPaths)
	cfg.Worktree.DiffExclude = trimPaths(cfg.Worktree.DiffExclude)
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
	for _, list := range [][]string{cfg.Worktree.CopyPaths, cfg.Worktree.SymlinkPaths, cfg.Worktree.DiffExclude} {
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

// requireWorktreeRepo holds a workflow that provisions worktrees to a cwd it can cut them
// from, since the worktree is weave's to make and it has nowhere to make it otherwise.
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

// ArchiveWorkflow archives a workflow and every agent inside it, since an agent cannot be left
// behind without a workflow to belong to.
func (w *WorkflowService) ArchiveWorkflow(ctx context.Context, id string) error {
	existing, err := w.GetWorkflow(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
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
	// Panes go before the commit, so a failed commit leaves rows the reconciler can still tidy
	// rather than a live CLI hidden for good.
	killOwnedPanes(ctx, w.tmux, inside)

	var taken []agentstore.AgentPane
	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		if err := workflowstore.New(db).Archive(ctx, id); err != nil {
			return err
		}

		archived, err := agentstore.New(db).ArchiveWorkflowAgents(ctx, id)
		taken = archived

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}
	// A pane recorded after inside was read only ever reaches here through what the transaction
	// took off the rows.
	killPanes(ctx, w.tmux, taken)

	notifyParents(ctx, w.notifier, inside, inside, "archived")

	return nil
}

// RestoreWorkflow brings an archived workflow back with every agent inside it, landing them
// dormant so a folder of twelve does not start twelve CLIs.
func (w *WorkflowService) RestoreWorkflow(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	existing, err := w.store.GetIncludingArchived(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q is not archived", ErrWorkflowNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	if existing.ArchivedAt == nil {
		return nil, fmt.Errorf("%w: %q is not archived", ErrWorkflowNotFound, id)
	}

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		agents := agentstore.New(db)

		archived, err := agents.ListArchivedInWorkflow(ctx, id)
		if err != nil {
			return err
		}
		// A partial restore would read as a whole one, so the ceiling and the names live agents
		// hold are both checked before anything is written.
		if err := claimRoomForAgents(ctx, agents, w.maxAgents, len(archived)); err != nil {
			return err
		}
		if err := claimRoomForRestoredChildren(ctx, agents, w.maxChildren, archived); err != nil {
			return err
		}
		if err := requireNamesFree(ctx, agents, archived); err != nil {
			return err
		}

		if err := workflowstore.New(db).Restore(ctx, id); err != nil {
			return err
		}

		if err := agents.RestoreWorkflowAgents(ctx, id); err != nil {
			return err
		}

		return stampRestoredPRs(ctx, agents, archived)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to restore workflow %q: %w", existing.Name, err)
	}

	return w.store.Get(ctx, id)
}

// DeleteWorkflow permanently removes an archived workflow along with whatever archived agents
// and schedules are still inside it.
func (w *WorkflowService) DeleteWorkflow(ctx context.Context, id string) error {
	existing, err := w.store.GetIncludingArchived(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q is not archived", ErrWorkflowNotFound, id)
	}
	if err != nil {
		return err
	}
	if existing.ArchivedAt == nil {
		return fmt.Errorf("%w: %q is not archived", ErrWorkflowNotFound, id)
	}

	schedules, err := w.emptied.heldSchedules(ctx, existing)
	if err != nil {
		return err
	}

	inside, err := w.agents.ListArchivedInWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if len(inside) == 0 {
		// No agent delete runs to check an empty workflow, so this is the one case that removes
		// the row here.
		return w.emptied.deleteWithSchedules(ctx, existing, schedules)
	}

	// Deleting an agent checks the workflow it leaves behind for itself, and leaves behind any
	// agent whose cleanup is still running or has failed.
	return w.agentDeleter.DeleteAgents(ctx, agentIDs(inside))
}

func requireNamesFree(ctx context.Context, agents nameChecker, archived []agentstore.Agent) error {
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

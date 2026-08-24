package agents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// worktreeSetupAction and worktreeCleanupAction are the built-in actions a worktree-enabled
// workflow's setup_action and cleanup_action are pointed at. Both are shipped scripts,
// internal/actions/builtin/_worktree_setup and _worktree_cleanup, that read the workflow's
// worktree settings off the environment rather than off a script generated for it, so one
// pair of actions serves every worktree-enabled workflow.
const (
	worktreeSetupAction   = "_worktree_setup"
	worktreeCleanupAction = "_worktree_cleanup"
)

// ErrWorkflowHasSchedules reports that a workflow still has schedules launching
// into it. A schedule cannot outlive its workflow, since the workflow is where its
// directory and flags come from.
var ErrWorkflowHasSchedules = errors.New("workflow still has schedules")

// ErrWorktreeNeedsCwd reports that a workflow turned worktree support on without naming the
// repo its worktrees are added off, which is the workflow's own cwd.
var ErrWorktreeNeedsCwd = errors.New("a worktree-enabled workflow needs a cwd naming the repo")

// ErrWorktreeOwnsActions reports that a workflow turned worktree support on while also
// naming a setup or cleanup action, which it does not need since it points at the built-in
// pair itself.
var ErrWorktreeOwnsActions = errors.New("a worktree-enabled workflow points setup_action and cleanup_action at the built-in pair itself, so both must be blank")

// ErrWorktreeActionsReserved reports that a workflow named the built-in worktree pair as
// its setup or cleanup action without the toggle that makes them work. The built-in
// scripts read the repo, base branch, and path lists off the environment, which is only
// populated when the toggle is on, so pointed at them any other way they fail every time.
var ErrWorktreeActionsReserved = errors.New("setup_action and cleanup_action may not name the built-in worktree pair unless worktree.enabled is set")

// ErrWorktreeUnsafePath reports that a worktree copy or symlink entry is one the generated
// setup action must not be handed.
var ErrWorktreeUnsafePath = errors.New("a worktree copy or symlink path must be a relative path inside the repo")

// workflowReadWriter is the part of the workflow store this service reads outside a
// transaction. Create and Update are not on it. Both write across three rows, so they
// run through InTx instead, over a store built fresh on the transaction's own handle.
type workflowReadWriter interface {
	List(ctx context.Context) ([]workflowstore.Workflow, error)
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

// scheduleCounter answers whether anything is still scheduled into a workflow,
// which is what stands between an archive and recurring work vanishing quietly.
type scheduleCounter interface {
	CountLive(ctx context.Context, workflowID string) (int64, error)
}

// workflowAgentReader reads the agents filed under a workflow, which an archive needs
// before it writes so it knows whose CLIs to stop.
type workflowAgentReader interface {
	ListLiveInWorkflow(ctx context.Context, workflowID string) ([]agentstore.Agent, error)
}

// WorkflowService manages the named buckets agents are grouped into. A workflow
// runs nothing. It holds a display name, shared context injected into the agents
// spawned inside it, and the cwd and flags those agents start from.
//
// Deleting one archives the agents inside it, and those rows belong to the agent
// domain, so that write runs through a transaction the two stores share. Archiving
// is also what stops those agents, which is why a service over pure organisation
// holds tmux at all.
type WorkflowService struct {
	store     workflowReadWriter
	agents    workflowAgentReader
	schedules scheduleCounter
	tmux      paneKiller
	tx        txRunner
	// maxAgents is the ceiling a restore has to fit the workflow's archived agents
	// under, zero for no ceiling.
	maxAgents int
	notifier  parentNotifier
}

// WorkflowParams carries the workflow service's dependencies.
type WorkflowParams struct {
	Workflows workflowReadWriter
	Agents    workflowAgentReader
	Schedules scheduleCounter
	Tmux      paneKiller
	Tx        txRunner
	MaxAgents int
	Notifier  parentNotifier
}

// NewWorkflowService builds the service from its dependencies.
func NewWorkflowService(p WorkflowParams) *WorkflowService {
	return &WorkflowService{
		store:     p.Workflows,
		agents:    p.Agents,
		schedules: p.Schedules,
		tmux:      p.Tmux,
		tx:        p.Tx,
		maxAgents: p.MaxAgents,
		notifier:  p.Notifier,
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

// namesReservedWorktreeAction reports whether a config names the built-in worktree pair
// as its own setup or cleanup action, which only means anything alongside the toggle that
// makes weave supply the environment those scripts read.
func namesReservedWorktreeAction(cfg workflowstore.Config) bool {
	return cfg.SetupAction == worktreeSetupAction || cfg.CleanupAction == worktreeCleanupAction
}

// normalizeWorktree trims the base branch and each path. The base branch and the two path
// lists are kept whatever the toggle is set to, on or off, so switching it back on later
// hands the workflow back what was there before rather than starting over blank.
func normalizeWorktree(cfg *workflowstore.Config) {
	cfg.Worktree.BaseBranch = strings.TrimSpace(cfg.Worktree.BaseBranch)
	cfg.Worktree.CopyPaths = trimPaths(cfg.Worktree.CopyPaths)
	cfg.Worktree.SymlinkPaths = trimPaths(cfg.Worktree.SymlinkPaths)
}

// trimPaths trims each entry and drops the blank ones, since a blank line in the form's
// textarea is a line the user left rather than a path they meant.
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

// validateWorktreePaths is the second layer under the RPC rules in proto/proto/rules/v1, which
// already hold every caller arriving over the API to a relative path. Entries are checked
// whether or not the toggle is on, since a list stored while it is off goes live the moment
// it is set.
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

	// Case-insensitively, since .GIT reaches the same directory on the filesystems weave runs on.
	if strings.EqualFold(segments[0], ".git") {
		return fmt.Errorf("%w: %q is the worktree's own link to the repo", ErrWorktreeUnsafePath, path)
	}

	return nil
}

// setWorktreeActions points a worktree-enabled config's setup and cleanup actions at the
// built-in pair, refusing one that already names its own since weave is about to overwrite
// it, and one with no cwd since that is the repo the built-in scripts work against.
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

	cfg.SetupAction = worktreeSetupAction
	cfg.CleanupAction = worktreeCleanupAction

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

	// A delegate spawned into this workflow can have a parent sitting in another one,
	// which archiving here would otherwise strand with no word its child is gone.
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

// requireNamesFree refuses when the archived agents coming back would collide on a name. The
// unique-name index covers the unarchived of one workflow, so a live agent may hold the
// name, or two of the returning agents may hold it between them.
//
// A name is only taken within its own workflow, so the counting keys on both. A batch
// restored by id can span folders, and two agents called auth in two of them are not a
// clash.
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

// agentName is a name and the workflow it is unique inside, which is the pair the
// unique-name index covers.
type agentName struct {
	workflowID string
	name       string
}

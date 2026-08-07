package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/config"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// ErrWorkflowHasSchedules reports that a workflow still has schedules launching
// into it. A schedule cannot outlive its workflow, since the workflow is where its
// directory and flags come from.
var ErrWorkflowHasSchedules = errors.New("workflow still has schedules")

// workflowReadWriter is the part of the workflow store this service reads and writes.
// Workflows are pure organisation, so this touches no tmux.
type workflowReadWriter interface {
	List(ctx context.Context) ([]workflowstore.Workflow, error)
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
	Create(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error)
	Update(ctx context.Context, id string, cfg workflowstore.Config) error
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
func (w *WorkflowService) NewWorkflow(ctx context.Context, cfg workflowstore.Config) (*workflowstore.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = config.ExpandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)
	cfg.SetupAction = strings.TrimSpace(cfg.SetupAction)
	cfg.CleanupAction = strings.TrimSpace(cfg.CleanupAction)

	created, err := w.store.Create(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return created, nil
}

// UpdateWorkflow replaces a workflow's editable fields. A blank name falls back
// to the one it already has, so the list never renders an unlabelled bucket.
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

	if err := w.store.Update(ctx, id, cfg); err != nil {
		return fmt.Errorf("failed to update workflow %q: %w", id, err)
	}

	return nil
}

// ArchiveWorkflow archives a workflow and the agents inside it. The agents go with
// it rather than being orphaned, since every agent belongs to a workflow and there
// is nowhere for one to be left.
//
// The agents' CLIs are stopped on the way, the same as archiving them by id, so nothing
// is left generating in a pane the panel no longer lists. The panes come down before the
// transaction rather than after, since an archive that killed them and then failed to
// commit leaves rows the reconciler already tidies, where one that committed and then
// died would hide a running CLI for good.
//
// A workflow still holding live schedules is refused. Archiving it would either
// strand them with nowhere to run or switch them off silently, so the schedules
// have to be turned off first, deliberately.
//
// The workflow row and the agent rows belong to different domains, so both
// stores are rebound onto one transaction. Either the bucket and its agents
// agree afterwards or nothing moved, since half of it landing would strand an
// agent behind a workflow nothing lists.
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

// RestoreWorkflow brings an archived workflow back with every agent inside it, the way a
// folder out of the trash carries its files, and returns it as stored. The agents land
// dormant, so a folder of twelve does not start twelve CLIs.
//
// It is all or nothing, under the agent ceiling and against the names live agents hold,
// since a workflow back with some of its runs would read as whole. A refusal names what
// is in the way so the user can clear it and press restore again.
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

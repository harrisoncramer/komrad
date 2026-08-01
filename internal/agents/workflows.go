package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/internal/store/workflow"
)

// ErrWorkflowHasSchedules reports that a workflow still has schedules launching
// into it. A schedule cannot outlive its workflow, since the workflow is where its
// directory and flags come from.
var ErrWorkflowHasSchedules = errors.New("workflow still has schedules")

// workflowStore is the part of the workflow store this service reads and writes.
// Workflows are pure organisation, so this touches no tmux.
type workflowStore interface {
	List(ctx context.Context) ([]workflow.Workflow, error)
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
	Create(ctx context.Context, cfg workflow.Config) (*workflow.Workflow, error)
	Update(ctx context.Context, id string, cfg workflow.Config) error
}

// scheduleCounter answers whether anything is still scheduled into a workflow,
// which is what stands between a delete and recurring work vanishing quietly.
type scheduleCounter interface {
	CountLive(ctx context.Context, workflowID string) (int64, error)
}

// WorkflowService manages the named buckets agents are grouped into. A workflow
// runs nothing. It holds a display name, shared context injected into the agents
// spawned inside it, and the cwd and flags those agents start from.
//
// Deleting one archives the agents inside it, and those rows belong to the agent
// domain, so that write runs through a transaction the two stores share.
type WorkflowService struct {
	store     workflowStore
	schedules scheduleCounter
	tx        txRunner
}

// WorkflowParams carries the workflow service's dependencies.
type WorkflowParams struct {
	Workflows workflowStore
	Schedules scheduleCounter
	Tx        txRunner
}

// NewWorkflowService builds the service from its dependencies.
func NewWorkflowService(p WorkflowParams) *WorkflowService {
	return &WorkflowService{store: p.Workflows, schedules: p.Schedules, tx: p.Tx}
}

// GetWorkflow returns a workflow by id, or nil when none has that id.
func (w *WorkflowService) GetWorkflow(ctx context.Context, id string) (*workflow.Workflow, error) {
	found, err := w.store.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	return found, nil
}

// NewWorkflow creates a workflow, defaulting a blank name so the list always has
// a label to render. The cwd is home-expanded so the stored blueprint matches
// what an agent launch would use.
func (w *WorkflowService) NewWorkflow(ctx context.Context, cfg workflow.Config) (*workflow.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
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
func (w *WorkflowService) UpdateWorkflow(ctx context.Context, id string, cfg workflow.Config) error {
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

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)
	cfg.SetupAction = strings.TrimSpace(cfg.SetupAction)
	cfg.CleanupAction = strings.TrimSpace(cfg.CleanupAction)

	if err := w.store.Update(ctx, id, cfg); err != nil {
		return fmt.Errorf("failed to update workflow %q: %w", id, err)
	}

	return nil
}

// DeleteWorkflow archives a workflow and the agents inside it. The agents go with
// it rather than being orphaned, since every agent belongs to a workflow and there
// is nowhere for one to be left.
//
// A workflow still holding live schedules is refused. Deleting it would either
// strand them with nowhere to run or switch them off silently, so the schedules
// have to be turned off first, deliberately.
//
// The workflow row and the agent rows belong to different domains, so both
// stores are rebound onto one transaction. Either the bucket and its agents
// agree afterwards or nothing moved, since half of it landing would strand an
// agent behind a workflow nothing lists.
func (w *WorkflowService) DeleteWorkflow(ctx context.Context, id string) error {
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

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		if err := workflow.New(db).Archive(ctx, id); err != nil {
			return err
		}

		return agentstore.New(db).ArchiveWorkflowAgents(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("failed to delete workflow %q: %w", id, err)
	}

	return nil
}

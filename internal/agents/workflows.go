package agents

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/store"
	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/store/workflow"
)

// workflowStore is the part of the workflow store this service reads and writes.
// Workflows are pure organisation, so this touches no tmux.
type workflowStore interface {
	List(ctx context.Context) ([]workflow.Workflow, error)
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
	Create(ctx context.Context, cfg workflow.Config) (*workflow.Workflow, error)
	Update(ctx context.Context, id string, cfg workflow.Config) error
}

// WorkflowService manages the named buckets agents are grouped into. A workflow
// runs nothing. It holds a display name, shared context injected into the agents
// spawned inside it, and the cwd and flags those agents start from.
//
// Deleting one has to say what becomes of the agents inside it, and those rows
// belong to the agent domain, so that write runs through a transaction the two
// stores share.
type WorkflowService struct {
	store workflowStore
	tx    txRunner
}

// WorkflowParams carries the workflow service's dependencies.
type WorkflowParams struct {
	Workflows workflowStore
	Tx        txRunner
}

// NewWorkflowService builds the service from its dependencies.
func NewWorkflowService(p WorkflowParams) *WorkflowService {
	return &WorkflowService{store: p.Workflows, tx: p.Tx}
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

	if err := w.store.Update(ctx, id, cfg); err != nil {
		return fmt.Errorf("failed to update workflow %q: %w", id, err)
	}

	return nil
}

// DeleteWorkflow archives a workflow. With archiveAgents the agents inside it are
// hidden alongside it, otherwise they are orphaned back to unassigned and keep
// running.
//
// The workflow row and the agent rows belong to different domains, so both stores
// are rebound onto one transaction. Either the bucket and its agents agree
// afterwards or nothing moved, since half of it landing would strand an agent
// behind a workflow nothing lists.
func (w *WorkflowService) DeleteWorkflow(ctx context.Context, id string, archiveAgents bool) error {
	existing, err := w.GetWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
	}

	err = w.tx.InTx(ctx, func(db store.DBTX) error {
		if err := workflow.New(db).Archive(ctx, id); err != nil {
			return err
		}

		agents := agentstore.New(db)
		if archiveAgents {
			return agents.ArchiveWorkflowAgents(ctx, id)
		}

		return agents.ClearWorkflow(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("failed to delete workflow %q: %w", id, err)
	}

	return nil
}

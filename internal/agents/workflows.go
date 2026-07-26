package agents

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/store"
	"claude-agents/internal/store/session"
	"claude-agents/internal/store/workflow"
)

// workflowStore is the part of the workflow store this service reads and writes.
// Workflows are pure organisation, so this touches no tmux at all.
type workflowStore interface {
	List(ctx context.Context) ([]workflow.Workflow, error)
	Get(ctx context.Context, id string) (*workflow.Workflow, error)
	Create(ctx context.Context, cfg workflow.Config) (*workflow.Workflow, error)
	Update(ctx context.Context, id string, cfg workflow.Config) error
}

// WorkflowService manages the named buckets chats are grouped into. A workflow
// runs nothing. It holds a display name, shared context injected into the chats
// spawned inside it, and the cwd and flags those chats start from.
//
// Deleting one has to say what becomes of the chats inside it, and those are the
// session domain's rows, so that write runs through a transaction the two stores
// share rather than either reaching into the other.
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
// what a session launch would use.
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

// DeleteWorkflow archives a workflow. With archiveChats the chats inside it are
// hidden alongside it, otherwise they are orphaned back to unassigned and keep
// running.
//
// The workflow row and the chat rows belong to different domains, so both stores
// are rebound onto one transaction: either the bucket and its chats agree
// afterwards, or nothing moved. Half of it landing is what would strand a chat
// behind a workflow nothing lists.
func (w *WorkflowService) DeleteWorkflow(ctx context.Context, id string, archiveChats bool) error {
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

		chats := session.New(db)
		if archiveChats {
			return chats.ArchiveWorkflowChats(ctx, id)
		}

		return chats.ClearWorkflow(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("failed to delete workflow %q: %w", id, err)
	}

	return nil
}

package agents

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/store"
)

// workflowStore is the part of the store the workflow service reads and writes.
// Workflows are pure organisation, so this touches no tmux at all.
type workflowStore interface {
	ListWorkflows(ctx context.Context) ([]store.Workflow, error)
	GetWorkflow(ctx context.Context, id string) (*store.Workflow, error)
	CreateWorkflow(ctx context.Context, cfg store.WorkflowConfig) (*store.Workflow, error)
	RenameWorkflow(ctx context.Context, id, name string) error
	UpdateWorkflow(ctx context.Context, id string, cfg store.WorkflowConfig) error
	DeleteWorkflow(ctx context.Context, id string) error
	DeleteWorkflowWithChats(ctx context.Context, id string) error
}

// WorkflowService manages the named buckets chats are grouped into. A workflow
// runs nothing; it holds a display name, shared context injected into the chats
// spawned inside it, and the cwd and flags those chats start from.
type WorkflowService struct {
	store workflowStore
}

// NewWorkflowService builds the service over a store.
func NewWorkflowService(s workflowStore) *WorkflowService {
	return &WorkflowService{store: s}
}

// ListWorkflows returns every live workflow for the tree's folder headers.
func (w *WorkflowService) ListWorkflows() ([]store.Workflow, error) {
	workflows, err := w.store.ListWorkflows(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	return workflows, nil
}

// GetWorkflow returns a workflow by id, or nil when it does not exist. The panel
// uses it to restore the workflow scope when looping back after an attach.
func (w *WorkflowService) GetWorkflow(id string) (*store.Workflow, error) {
	workflow, err := w.store.GetWorkflow(context.Background(), id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	return workflow, nil
}

// NewWorkflow creates a workflow with its free-text context and default config,
// defaulting a blank name so the list always has a label. The name is a display
// label only and nothing keys on it. The cwd is home-expanded so the stored
// blueprint matches what a session launch would use.
func (w *WorkflowService) NewWorkflow(cfg store.WorkflowConfig) (*store.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	workflow, err := w.store.CreateWorkflow(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return workflow, nil
}

// RenameWorkflow retitles a workflow by id. A blank or unchanged name is a no-op.
func (w *WorkflowService) RenameWorkflow(workflow store.Workflow, name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == workflow.Name {
		return
	}

	_ = w.store.RenameWorkflow(context.Background(), workflow.ID, name)
}

// UpdateWorkflow patches a workflow's display name, free-text context, and
// default config in place, keyed by id. A blank name falls back to the existing
// name so the list always has a label. The cwd is home-expanded so the stored
// blueprint matches what a session launch would use.
func (w *WorkflowService) UpdateWorkflow(workflow store.Workflow, cfg store.WorkflowConfig) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = workflow.Name
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	_ = w.store.UpdateWorkflow(context.Background(), workflow.ID, cfg)
}

// DeleteWorkflow soft-deletes a workflow, orphaning its chats back to
// unassigned so none are stranded behind it.
func (w *WorkflowService) DeleteWorkflow(workflow store.Workflow) {
	_ = w.store.DeleteWorkflow(context.Background(), workflow.ID)
}

// DeleteWorkflowWithChats soft-deletes a workflow together with its chats, so
// the whole bucket is removed at once rather than orphaning its chats.
func (w *WorkflowService) DeleteWorkflowWithChats(workflow store.Workflow) {
	_ = w.store.DeleteWorkflowWithChats(context.Background(), workflow.ID)
}

// MoveChat reassigns a chat to a workflow, or unassigns it when workflowID is
// nil, keyed by the chat's stable id.

// UpdateWorkflowByID replaces a workflow's editable fields, keyed by id. It
// exists alongside UpdateWorkflow because a caller off the wire holds an id
// rather than a record, and the id may name nothing.
func (w *WorkflowService) UpdateWorkflowByID(id string, cfg store.WorkflowConfig) error {
	existing, err := w.GetWorkflow(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
	}

	w.UpdateWorkflow(*existing, cfg)

	return nil
}

// DeleteWorkflowByID removes a workflow, keyed by id. With archiveChats set the
// chats inside it are hidden alongside it; otherwise they are orphaned back to
// unassigned and keep running.
func (w *WorkflowService) DeleteWorkflowByID(id string, archiveChats bool) error {
	existing, err := w.GetWorkflow(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("%w: %q", ErrWorkflowNotFound, id)
	}

	if archiveChats {
		w.DeleteWorkflowWithChats(*existing)
		return nil
	}

	w.DeleteWorkflow(*existing)

	return nil
}

package agents

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/store"
)

// ListWorkflows returns every live workflow for the tree's folder headers.
func (m *AgentManager) ListWorkflows() ([]store.Workflow, error) {
	workflows, err := m.sessions.ListWorkflows(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	return workflows, nil
}

// GetWorkflow returns a workflow by id, or nil when it does not exist. The panel
// uses it to restore the workflow scope when looping back after an attach.
func (m *AgentManager) GetWorkflow(id string) (*store.Workflow, error) {
	workflow, err := m.sessions.GetWorkflow(context.Background(), id)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	return workflow, nil
}

// NewWorkflow creates a workflow with its free-text context and default config,
// defaulting a blank name so the list always has a label. The name is a display
// label only and nothing keys on it. The cwd is home-expanded so the stored
// blueprint matches what a session launch would use.
func (m *AgentManager) NewWorkflow(cfg store.WorkflowConfig) (*store.Workflow, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "workflow"
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	workflow, err := m.sessions.CreateWorkflow(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	return workflow, nil
}

// RenameWorkflow retitles a workflow by id. A blank or unchanged name is a no-op.
func (m *AgentManager) RenameWorkflow(workflow store.Workflow, name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == workflow.Name {
		return
	}

	_ = m.sessions.RenameWorkflow(context.Background(), workflow.ID, name)
}

// UpdateWorkflow patches a workflow's display name, free-text context, and
// default config in place, keyed by id. A blank name falls back to the existing
// name so the list always has a label. The cwd is home-expanded so the stored
// blueprint matches what a session launch would use.
func (m *AgentManager) UpdateWorkflow(workflow store.Workflow, cfg store.WorkflowConfig) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = workflow.Name
	}

	cfg.Cwd = expandHome(strings.TrimSpace(cfg.Cwd))
	cfg.Flags = strings.TrimSpace(cfg.Flags)

	_ = m.sessions.UpdateWorkflow(context.Background(), workflow.ID, cfg)
}

// DeleteWorkflow soft-deletes a workflow, orphaning its chats back to
// unassigned so none are stranded behind it.
func (m *AgentManager) DeleteWorkflow(workflow store.Workflow) {
	_ = m.sessions.DeleteWorkflow(context.Background(), workflow.ID)
}

// DeleteWorkflowWithChats soft-deletes a workflow together with its chats, so
// the whole bucket is removed at once rather than orphaning its chats.
func (m *AgentManager) DeleteWorkflowWithChats(workflow store.Workflow) {
	_ = m.sessions.DeleteWorkflowWithChats(context.Background(), workflow.ID)
}

// MoveChat reassigns a chat to a workflow, or unassigns it when workflowID is
// nil, keyed by the chat's stable id.

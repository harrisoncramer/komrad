package ui

import (
	"fmt"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// agentRename is a sidebar row's name being typed over.
type agentRename struct {
	*nameEdit
	agent    panel.Agent
	workflow *panel.Workflow
}

func (a *agentsScreen) isRenaming() bool {
	return a.renaming != nil
}

func (a *agentsScreen) openRename(s panelShared) tea.Cmd {
	row, ok := a.hoveredRow(s)
	if !ok {
		return nil
	}

	rename := agentRename{nameEdit: openNameEdit(rowKey(row), row.item.Agent.Name), agent: row.item.Agent}
	if row.header {
		workflow := row.workflow
		rename = agentRename{nameEdit: openNameEdit(rowKey(row), workflow.Name), workflow: &workflow}
	}

	a.renaming = &rename

	return nil
}

// reconcileRename drops an open rename whose row the cursor is no longer on.
func (a *agentsScreen) reconcileRename(s panelShared) {
	if a.renaming == nil || a.renaming.saving || a.hoveredKey(s) == a.renaming.key {
		return
	}

	a.renaming = nil
}

func (a *agentsScreen) updateRename(h host, msg tea.Msg) tea.Cmd {
	switch a.renaming.take(msg) {
	case nameEditHidden:
		return h.hide()
	case nameEditCancelled:
		a.renaming = nil
	case nameEditSubmitted:
		return a.saveRename(h)
	case nameEditTyping:
	}

	return nil
}

func (a *agentsScreen) saveRename(h host) tea.Cmd {
	rename := a.renaming
	name, ok := rename.submit()
	if !ok {
		a.renaming = nil

		return nil
	}

	if rename.workflow != nil {
		renamed := *rename.workflow
		renamed.Name = name

		return stampRename(h.writeCmd(
			fmt.Sprintf("Could not rename workflow %q", rename.was),
			func(a api) error { return a.updateWorkflow(renamed) },
		), rename.key)
	}

	agent := rename.agent

	return stampRename(h.writeCmd(
		fmt.Sprintf("Could not rename agent %q", rename.was),
		func(a api) error { return a.renameAgent(agent, name) },
	), rename.key)
}

// settleRename closes the field once its write has landed, and opens it again for another try
// when the write failed.
func (a *agentsScreen) settleRename(s panelShared, row string, err error) {
	if a.renaming == nil {
		return
	}
	if !a.renaming.settle(row, err) {
		a.renaming = nil

		return
	}

	a.reconcileRename(s)
}

func (a *agentsScreen) renamingField() *nameEdit {
	if a.renaming == nil {
		return nil
	}

	return a.renaming.nameEdit
}

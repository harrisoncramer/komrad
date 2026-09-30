package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// agentRename is a row's name being typed over in the sidebar.
type agentRename struct {
	key      string
	agent    panel.Agent
	workflow *panel.Workflow
	typed    string
	// saving says the typed name has been sent and the field takes no keys until it lands.
	saving bool
}

func (r agentRename) name() string {
	if r.workflow != nil {
		return r.workflow.Name
	}

	return r.agent.Name
}

func (a *agentsScreen) isRenaming() bool {
	return a.renaming != nil
}

func (a *agentsScreen) openRename(s panelShared) tea.Cmd {
	row, ok := a.hoveredRow(s)
	if !ok {
		return nil
	}

	rename := agentRename{key: rowKey(row), agent: row.item.Agent, typed: row.item.Agent.Name}
	if row.header {
		workflow := row.workflow
		rename = agentRename{key: rowKey(row), workflow: &workflow, typed: workflow.Name}
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
	rename := a.renaming
	if rename.saving {
		return nil
	}

	switch msg := msg.(type) {
	case tea.PasteMsg:
		rename.typed += pastedLine(msg)
	case tea.KeyPressMsg:
		switch k := msg.String(); {
		case k == "ctrl+c":
			return h.hide()
		case k == "esc":
			a.renaming = nil
		case k == "enter":
			return a.saveRename(h)
		case k == "backspace":
			rename.typed = paint.TrimLastRune(rename.typed)
		case typedText(msg) != "":
			rename.typed += typedText(msg)
		}
	}

	return nil
}

func (a *agentsScreen) saveRename(h host) tea.Cmd {
	rename := a.renaming
	name := strings.TrimSpace(rename.typed)
	if name == "" || name == rename.name() {
		a.renaming = nil

		return nil
	}

	rename.typed = name
	rename.saving = true

	if rename.workflow != nil {
		renamed := *rename.workflow
		renamed.Name = name

		return stampRename(h.writeCmd(
			fmt.Sprintf("Could not rename workflow %q", rename.workflow.Name),
			func(a api) error { return a.updateWorkflow(renamed) },
		), rename.key)
	}

	agent := rename.agent

	return stampRename(h.writeCmd(
		fmt.Sprintf("Could not rename agent %q", agent.Name),
		func(a api) error { return a.renameAgent(agent, name) },
	), rename.key)
}

// stampRename says on a write's result which row's rename it carried.
func stampRename(cmd tea.Cmd, row string) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.renamed = row

		return result
	}
}

// settleRename closes the field once its write has landed, and opens it again for another try
// when the write failed.
func (a *agentsScreen) settleRename(s panelShared, row string, err error) {
	if a.renaming == nil || !a.renaming.saving || a.renaming.key != row {
		return
	}

	if err == nil {
		a.renaming = nil

		return
	}

	a.renaming.saving = false
	a.reconcileRename(s)
}

func renameHelp() keyMap {
	save := binding([]string{"enter"}, "save")
	cancel := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{save, cancel}, full: []key.Binding{save, cancel}}
}

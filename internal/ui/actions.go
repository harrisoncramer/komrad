package ui

import (
	"errors"
	"fmt"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
)

var errNotText = errors.New("the file is not text, so there is nothing to edit")

type actionState struct {
	items  []actionItem
	cursor int
	agent  agentstore.Agent
}

type actionItem struct {
	name        string
	executable  bool
	description string
	timeout     time.Duration
	problems    []string

	text bool
}

type actionsResult struct {
	actions []actionItem
	err     error
}

func (m model) enterActions() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	m.actions = actionState{agent: item.agent}
	m.enterScreen(screenActions)
	m.stageAction()

	return m, m.listActionsCmd()
}

func (m model) actionRows() []actionItem {
	if m.filter.pattern == nil {
		return m.actions.items
	}

	return xslice.Filter(m.actions.items, func(action actionItem) bool {
		return m.filter.doesMatch(action.name)
	})
}

func (m model) listActionsCmd() tea.Cmd {
	clients := m.api

	return func() tea.Msg {
		listed, err := clients.listActions()
		if err != nil {
			return actionsResult{err: err}
		}

		return actionsResult{actions: actionItemsFrom(listed)}
	}
}

func (m *model) applyActions(result actionsResult) {
	if m.screen != screenActions {
		return
	}

	if result.err != nil {
		m.report("Could not read your actions", result.err)

		return
	}

	m.actions.items = result.actions
	m.actions.cursor = clampCursor(m.actions.cursor, len(m.actionRows()))
	m.stageAction()
}

func (m model) updateActions(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Actions.Quit):
		return m, m.hide()
	case m.filter.standing() && isBound(k, m.keys.Actions.Back):
		return m.openFilter()
	case isBound(k, m.keys.Actions.Back):
		return m.returnToAgents()
	case isBound(k, m.keys.Nav.Up):
		if next, ok := stepCursor(m.actions.cursor, -1, len(m.actionRows())); ok {
			m.actions.cursor = next
			m.stageAction()
		}
	case isBound(k, m.keys.Nav.Down):
		if next, ok := stepCursor(m.actions.cursor, 1, len(m.actionRows())); ok {
			m.actions.cursor = next
			m.stageAction()
		}
	case isBound(k, m.keys.Actions.Run):
		return m.runSelectedAction()
	case isBound(k, m.keys.Actions.Editor):
		return m.editSelectedAction()
	}

	return m, nil
}

func (m model) selectedAction() (actionItem, bool) {
	return rowAt(m.actionRows(), m.actions.cursor)
}

func (m model) runSelectedAction() (tea.Model, tea.Cmd) {
	action, ok := m.selectedAction()
	if !ok {
		return m, nil
	}

	agentID, name := m.actions.agent.ID, action.name

	return m, m.writeCmd(
		fmt.Sprintf("Could not run action %q", name),
		func(a api) error { return a.runAction(agentID, name) },
	)
}

func (m model) editSelectedAction() (tea.Model, tea.Cmd) {
	action, ok := m.selectedAction()
	if !ok {
		return m, nil
	}

	if m.info.kind == infoEdit && m.info.action == action.name {
		return m.closeInfo(), nil
	}

	summary := fmt.Sprintf("Could not edit action %q", action.name)
	if !action.text {
		m.report(summary, errNotText)

		return m, nil
	}

	if !m.claimInfo(infoEdit, action.name) {
		return m, nil
	}

	agentID, name := m.actions.agent.ID, action.name

	return m, m.writeCmd(summary, func(a api) error { return a.editAction(agentID, name) })
}

const noActionsMarkdown = "# Actions\n\n_nothing here yet_\n\n" +
	"An action is any executable file in your actions directory, which is " +
	"`~/.config/weave/actions` unless your config names another.\n\n" +
	"It runs on the agent you opened this list from, in that directory rather than the " +
	"agent's, with the agent as JSON in `WEAVE_AGENT`. Where its output goes is up to it.\n\n" +
	"A comment at the top of the file saying `#WEAVE description=\"what it does\"` shows up " +
	"here, and `#WEAVE timeout=\"90s\"` is how long it gets before weave stops it.\n\n" +
	"A file whose name starts with `_` is left off this list and out of the workflow form, " +
	"which is how shell your other actions source stays out of the way.\n"

func (m model) stageAction() {
	action, ok := m.selectedAction()
	if !ok {
		m.frame.Show(layout.RegionStage, newTextContent(noActionsMarkdown))

		return
	}

	m.frame.Show(layout.RegionStage, newTextContent(actionMarkdown(action)))
}

func (m model) actionsView() string {
	help := "\n" + m.renderHelp(m.actionHelp())

	all := m.actionRows()
	if len(all) == 0 {
		return m.listView("Run Action", []string{dimStyle.Render("no actions")}, 0, help)
	}

	rows := make([]string, 0, len(all))
	for i, action := range all {
		hovered := i == m.actions.cursor
		prefix := "  "
		if hovered {
			prefix = m.rowCursorStyle().Render("› ")
		}

		name := m.filter.highlight(action.name, m.nameStyle(hovered, !action.executable))
		rows = append(rows, prefix+name)
	}

	return m.listView("Run Action", rows, m.actions.cursor, help)
}

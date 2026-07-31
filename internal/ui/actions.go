package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// actionState is the actions screen, the scripts the user has written for the panel to run.
// The list is asked for on the way in rather than polled, since a directory does not change
// often enough to restat every second the screen is up.
//
// agent is the agent the screen was opened on, held here rather than read back off the cursor
// when a script is chosen. The refresh keeps running while the list is up and can move the rows
// under it, so reading the cursor then could fire the action at whatever the last pass left
// there.
type actionState struct {
	items  []actionItem
	cursor int
	agent  agentstore.Agent
}

// actionsResult is a finished listing. It is its own message rather than part of the refresh
// pass, since the pass runs every second and this is read once per visit to the screen.
type actionsResult struct {
	actions []actionItem
	err     error
}

// enterActions opens the actions list on the hovered agent. The list arrives as a message, so
// the screen paints empty and fills in, the way the trash does.
func (m model) enterActions() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	m.actions = actionState{agent: item.agent}
	m.screen = screenActions
	m.stageAction()

	return m, m.listActionsCmd()
}

// listActionsCmd asks the service for the actions, off the render loop like every other call
// the panel makes.
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

// applyActions puts a finished listing on screen. A listing that failed is reported and leaves
// the list empty, since there is nothing to show and the stage would otherwise claim the
// directory is empty when nobody could read it.
func (m *model) applyActions(result actionsResult) {
	if result.err != nil {
		m.report("Could not read your actions", result.err)

		return
	}

	m.actions.items = result.actions
	m.actions.cursor = clampCursor(m.actions.cursor, len(m.actions.items))
	m.stageAction()
}

// updateActions drives the actions list. Nav moves the selection and restages, and run fires
// the action under the cursor at the agent the list was opened on.
func (m model) updateActions(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Actions.Quit):
		return m, m.hide()
	case bound(k, m.keys.Actions.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.actions.cursor > 0 {
			m.actions.cursor--
			m.stageAction()
		}
	case bound(k, m.keys.Nav.Down):
		if m.actions.cursor < len(m.actions.items)-1 {
			m.actions.cursor++
			m.stageAction()
		}
	case bound(k, m.keys.Actions.Run):
		return m.runSelectedAction()
	}

	return m, nil
}

// selectedAction returns the action under the cursor, or false when there are none.
func (m model) selectedAction() (actionItem, bool) {
	if m.actions.cursor < 0 || m.actions.cursor >= len(m.actions.items) {
		return actionItem{}, false
	}

	return m.actions.items[m.actions.cursor], true
}

// runSelectedAction fires the action under the cursor at the agent the list was opened on.
// Nothing waits for it and nothing reports what it did, since where its output goes is the
// action's to decide, and whatever it changes reaches the panel the way a change made from
// anywhere else does, on the next refresh.
//
// The list stays where it is. Firing is closer to a webhook than to opening something, so there
// is nothing to navigate to, and staying put leaves the cursor on the agent to fire a second
// action at.
//
// A failure is always from before the process started, an action or agent that is not there or
// a file the service could not execute, so it banners over the list the row is on.
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

// noActionsMarkdown is what the stage shows with nothing to list, so the pane says what an
// action is rather than sitting blank. It names no directory: the panel does not resolve one
// any more, and the config is where the answer lives.
const noActionsMarkdown = "# Actions\n\n_nothing here yet_\n\n" +
	"An action is any executable file in your actions directory, which is " +
	"`~/.config/weave/actions` unless your config names another.\n\n" +
	"It runs on the agent you opened this list from, in that agent's directory, with the " +
	"agent as JSON in `WEAVE_AGENT`. Where its output goes is up to it.\n"

// stageAction shows the action under the cursor in the stage. The service sends the start of
// each file with the listing, so hovering costs nothing and the panel never reads the directory
// itself.
func (m model) stageAction() {
	action, ok := m.selectedAction()
	if !ok {
		m.stage.Text(noActionsMarkdown)

		return
	}

	m.stage.Text(actionMarkdown(action))
}

// actionsView renders the actions list, naming the agent they would run on. The list acts on the
// row it was opened from rather than on whatever the agents list is hovering by the time an
// action is chosen, so the name says which agent it has. A file without the executable bit is
// drawn dim, the way a dormant agent is, since it is there and it will not run.
func (m model) actionsView() string {
	header := headerStyle.Render("Actions on "+m.actions.agent.Name) + "\n\n"
	hints := "\n" + m.actionHints()

	if len(m.actions.items) == 0 {
		return header + dimStyle.Render("no actions") + "\n" + hints
	}

	rows := make([]string, 0, len(m.actions.items))
	for i, action := range m.actions.items {
		prefix := "  "
		name := action.name
		if !action.executable {
			name = dimStyle.Render(name)
		}
		if i == m.actions.cursor {
			prefix = cursorStyle.Render("› ")
			name = cursorStyle.Render(action.name)
		}

		rows = append(rows, prefix+name)
	}

	return header + renderList(rows, m.actions.cursor, m.listLines(header, hints)) + hints
}

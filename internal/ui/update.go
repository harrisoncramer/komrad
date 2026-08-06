package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The messages coming in. Init starts the loops and Update routes whatever arrives to
// the screen that owns it. view.go is the other end, the strings going out, and
// model.go is the state between them.

// hide detaches the popup's client from the persistent manager session without
// stopping the panel. The bubbletea program keeps running with its state intact,
// so reopening the popup restores the exact screen. The close and quit keys hide
// the popup rather than tear the panel down.
func (m model) hide() tea.Cmd {
	return func() tea.Msg {
		m.popup.Hide()
		return nil
	}
}

// fatal is the failure that replaces the panel, or nil when there is none. Both
// are settled before the panel starts and neither can clear itself, so either one
// means there is no panel behind the screen it shows. A config that would not parse
// comes first: a panel that never read its keybindings never got as far as trying
// to start a server.
func (m model) fatal() error {
	if m.configErr != nil {
		return m.configErr
	}

	return m.startupErr
}

// Init starts the refresh loop, which is what fills the panel in and then keeps its
// rows and states current. A startup failure means there is no list to refresh, so
// nothing is started.
func (m model) Init() tea.Cmd {
	if m.fatal() != nil {
		return nil
	}

	return tea.Batch(refreshTick(), m.refreshCmd())
}

// Update routes a message to the screen that owns it.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.route(msg)
}

// route hands a message to the screen that owns it.
func (m model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A resize is recorded and then falls through rather than being consumed, since
	// an open form lays itself out from the same message. It is taken before the
	// fatal screens, which consume everything else: a screen that never learned the
	// pane it is in folds its error to a width the pane cannot hold, and the renderer
	// cuts what does not fit at the pane edge.
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height
		m.width = size.Width
	}

	// A startup failure screen is terminal: there is nothing to act on, so any key
	// dismisses it. Quitting exits zero, which is what lets the popup close on the
	// keypress rather than lingering.
	if m.fatal() != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, tea.Quit
		}

		return m, nil
	}

	// The banner is chrome rather than a screen, so it is dismissed with esc as a
	// literal rather than through the rebindable back binding: back also carries
	// left and h, and navigation keys should move the cursor, not clear an error.
	// Form screens keep their own esc, which aborts the form.
	if m.banner != nil && !m.screen.isForm() {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.banner = nil

			return m, nil
		}
	}

	if next, cmd, handled := m.routeAsyncMessage(msg); handled {
		return next, cmd
	}

	// A form waiting on its write takes no keys, so a second submit cannot send the same
	// request twice. Every write is bounded by its own timeout, so the form is never held
	// for longer than that.
	if _, ok := msg.(tea.KeyMsg); ok && m.pendingForm {
		return m, nil
	}

	if m.screen.isForm() {
		return m.routeFormMessage(msg)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	// An open filter input owns every key, ahead of the help toggle and the screens below,
	// since a letter that acted as well as typed could be used for neither.
	if m.filter.typing {
		return m.updateFilterInput(key)
	}

	if !m.isQueryOpen() && isBound(key.String(), m.keys.Help.Toggle) {
		m.helpExpanded = !m.helpExpanded

		return m, nil
	}

	// The log is about the service rather than about the list on screen, so it is answered
	// here rather than by each screen. It is wanted when the service is misbehaving, which is
	// as likely to be noticed from the schedules or the archive as from the agents list.
	if !m.isQueryOpen() && isBound(key.String(), m.keys.Logs.Open) {
		return m.openLogs()
	}

	// The config is about the panel rather than about the list on screen, so it is answered
	// here for the reason the log is. It is taken ahead of the screens because the bindings
	// it is about are the ones they would read it with.
	if !m.isQueryOpen() && isBound(key.String(), m.keys.Config.Reload) {
		return m, m.reloadCmd()
	}

	switch m.screen {
	case screenSearch:
		return m.updateSearch(key)
	case screenSchedules:
		return m.updateSchedules(key)
	case screenArchive:
		return m.updateArchive(key)
	case screenActions:
		return m.updateActions(key)
	case screenAgents:
		return m.updateAgents(key)
	default:
		return m, nil
	}
}

func (m model) routeAsyncMessage(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case refreshTickMsg:
		return m, tea.Batch(m.refreshCmd(), refreshTick()), true
	case refreshResult:
		m.applyRefresh(msg)

		return m, nil, true
	case writeResult:
		next, cmd := m.applyWrite(msg)

		return next, cmd, true
	case searchSettledMsg:
		return m, m.runSettledSearch(msg), true
	case searchResult:
		return m, m.applySearch(msg), true
	case actionsResult:
		m.applyActions(msg)

		return m, nil, true
	case previewResult:
		m.applyPreview(msg)

		return m, nil, true
	case copyResult:
		m.applyCopy(msg)

		return m, nil, true
	case flashRevertMsg:
		m.restageScreen()

		return m, nil, true
	case reloadResult:
		next, cmd := m.applyReload(msg)

		return next, cmd, true
	default:
		return m, nil, false
	}
}

func (m model) routeFormMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenForm:
		return m.updateForm(msg)
	case screenRename:
		return m.updateRename(msg)
	case screenAgentArchive:
		return m.updateAgentArchive(msg)
	case screenWorkflowForm:
		return m.updateWorkflowForm(msg)
	case screenWorkflowArchive:
		return m.updateWorkflowArchive(msg)
	case screenScheduleForm:
		return m.updateScheduleForm(msg)
	case screenScheduleArchive:
		return m.updateScheduleArchive(msg)
	case screenArchiveDelete:
		return m.updateArchiveDelete(msg)
	default:
		return m, nil
	}
}

// isQueryOpen reports whether the search screen's query line has the keys, which is the one
// state where a key bound to something has to type instead of acting.
func (m model) isQueryOpen() bool {
	return m.screen == screenSearch && m.search.typing
}

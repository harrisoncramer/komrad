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

// Update routes a message and then squares the layout with the screen it left the panel
// on. A form is a takeover, so the stage is zoomed away for as long as one is up. This is
// reconciled from the screen after the fact rather than at each of the dozen places a form
// opens or closes, so a new form cannot be added that forgets to do it.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.route(msg)

	if updated, ok := next.(model); ok && updated.stage != nil {
		updated.stage.Hide(updated.screen.isForm())
	}

	return next, cmd
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

	switch msg := msg.(type) {
	case refreshTickMsg:
		return m, tea.Batch(m.refreshCmd(), refreshTick())
	case refreshResult:
		m.applyRefresh(msg)

		return m, nil
	case writeResult:
		return m, m.applyWrite(msg)
	case searchSettledMsg:
		return m, m.runSettledSearch(msg)
	case searchResult:
		return m, m.applySearch(msg)
	case actionsResult:
		m.applyActions(msg)

		return m, nil
	case previewResult:
		m.applyPreview(msg)

		return m, nil
	}

	// The form screens take every message, since a form lays itself out from a resize
	// as much as it reads a keypress.
	switch m.screen {
	case screenForm:
		return m.updateForm(msg)
	case screenRename:
		return m.updateRename(msg)
	case screenWorkflowForm:
		return m.updateWorkflowForm(msg)
	case screenWorkflowArchive:
		return m.updateWorkflowArchive(msg)
	case screenScheduleForm:
		return m.updateScheduleForm(msg)
	case screenScheduleDelete:
		return m.updateScheduleDelete(msg)
	case screenArchiveDelete:
		return m.updateArchiveDelete(msg)
	default:
		// The list screens act on keys alone, which are dispatched below.
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.screen != screenSearch && bound(key.String(), m.keys.Help.Toggle) {
		m.helpCollapsed = !m.helpCollapsed

		return m, nil
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

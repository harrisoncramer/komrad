package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) hide() tea.Cmd {
	return func() tea.Msg {
		m.popup.Hide()
		return nil
	}
}

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

func (m model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.height = size.Height
		m.width = size.Width
	}

	if m.fatal() != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, tea.Quit
		}

		return m, nil
	}

	if m.banner != nil && !m.screen.isForm() {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.banner = nil

			return m, nil
		}
	}

	if next, cmd, handled := m.routeAsyncMessage(msg); handled {
		return next, cmd
	}

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

	if m.filter.typing {
		return m.updateFilterInput(key)
	}

	// Something being typed into owns the keyboard. Every global key is also a character
	// somebody means to type, so none of them are bound until the input closes and the screen
	// gets the keys on its own terms.
	if m.isTyping() {
		return m.routeScreenKey(key)
	}

	if isBound(key.String(), m.keys.Help.Toggle) {
		m.helpExpanded = !m.helpExpanded

		return m, nil
	}

	if m.screen != screenReview && isBound(key.String(), m.keys.Logs.Open) {
		return m.cycleLogs()
	}

	if m.detailsScreen() {
		if m.detailsFocused() {
			return m.updateDetails(key)
		}
		if isBound(key.String(), m.keys.Details.Focus) {
			return m.toggleDetailsFocus()
		}
	}

	return m.routeScreenKey(key)
}

func (m model) routeScreenKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenSearch:
		return m.updateSearch(key)
	case screenSchedules:
		return m.updateSchedules(key)
	case screenArchive:
		return m.updateArchive(key)
	case screenReview:
		return m.updateReview(key)
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
		if m.applyRefresh(msg) {
			next, cmd := m.reloadReview()

			return next, cmd, true
		}

		return m, nil, true
	case writeResult:
		next, cmd := m.applyWrite(msg)

		return next, cmd, true
	case reviewLoadedMsg:
		m.applyReviewLoaded(msg)

		return m, nil, true
	case reviewWroteMsg:
		m.applyReviewWrote(msg)

		return m, nil, true
	case searchSettledMsg:
		return m, m.runSettledSearch(msg), true
	case searchResult:
		return m, m.applySearch(msg), true
	case previewResult:
		m.applyPreview(msg)

		return m, nil, true
	case copyResult:
		m.applyCopy(msg)

		return m, nil, true
	case flashRevertMsg:
		m.details.flashed = 0
		m.restageScreen()

		return m, nil, true
	default:
		return m, nil, false
	}
}

func (m model) routeFormMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenForm:
		return m.updateForm(msg)
	case screenAdopt:
		return m.updateAdopt(msg)
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
	case screenArchiveEmpty:
		return m.updateArchiveEmpty(msg)
	default:
		return m, nil
	}
}

// isTyping reports whether the panel's own bindings are off: a text input is up taking
// characters, or a key prefix is waiting on the key that completes it.
func (m model) isTyping() bool {
	return m.isQueryOpen() || (m.screen == screenReview && m.review.takesEveryKey())
}

func (m model) isQueryOpen() bool {
	return m.screen == screenSearch && m.search.typing
}

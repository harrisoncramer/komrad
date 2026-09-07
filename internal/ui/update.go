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

// Init starts the refresh loop that fills the panel in and keeps it current, unless startup
// failed and there is no list to refresh.
func (m model) Init() tea.Cmd {
	if m.fatal() != nil {
		return nil
	}

	m.popup.HoldFocusKey(m.focusKey, m.pane, m.screen == screenAgents)

	return tea.Batch(refreshTick(), m.refreshCmd())
}

// Update routes a message to the screen that owns it.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.route(msg)
}

func (m model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		// The transcript is wrapped into the column it is read in rather than into a pane that
		// would fold it, so only a window that changed width has to wrap it again, and a preview
		// that has never drawn is one the rows are still owed to.
		rewrap := size.Width != m.width || len(m.search.preview.doc.rows) == 0

		m.height = size.Height
		m.width = size.Width

		if rewrap {
			m.drawPreview()
		}
	}

	if m.fatal() != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, tea.Quit
		}

		return m, nil
	}

	// A mark binding owns the key after it, whatever that key is otherwise bound to, and takes it
	// ahead of the banner's own escape, which would otherwise eat the press that backs out of a
	// mark and leave the panel waiting for a letter with nothing on screen saying so.
	if key, ok := msg.(tea.KeyMsg); ok && m.marks.pending != markPendingNone {
		return m.readMark(key)
	}

	if m.banner != nil && !m.form.isActive() {
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

	if m.form.isActive() {
		// A resize says how big the panel's window is, and the form is drawn in a column of it, so
		// the size is rewritten here rather than in each form's own handler.
		return m.form.update(m, m.formSizeOf(msg))
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.filter.typing {
		return m.updateFilterInput(key)
	}

	// Something being typed into owns the keyboard, since every global key is also a character
	// somebody means to type.
	if m.isTyping() {
		return m.routeScreenKey(key)
	}

	if isBound(key.String(), m.keys.Help.Toggle) {
		m.helpExpanded = !m.helpExpanded

		return m, nil
	}

	// Whether these keys act is a policy about the keys rather than the geometry holdsWindow
	// answers, which the archive is what separates: it takes the whole window and hands it back
	// for as long as the pane is up, since a failed teardown is the thing its own logs explain.
	if m.screen.opensInfoPane() && isBound(key.String(), m.keys.Logs.Open) {
		return m.cycleLogs()
	}

	if m.screen.opensInfoPane() && isBound(key.String(), m.keys.Logs.Older) {
		return m.olderLog()
	}

	if m.screen.opensInfoPane() && isBound(key.String(), m.keys.Terminal.Open) {
		return m.openTerminal()
	}

	// The block beside the list is the panel's own to scroll, so the keys that move it are routed
	// here rather than repeated on every screen that draws one.
	if direction, ok := m.isColumnScroll(key.String()); ok {
		return m.scrollColumn(direction)
	}

	if m.isDetailsScreen() {
		if m.isDetailsFocused() {
			return m.updateDetails(key)
		}
		if isBound(key.String(), m.keys.Details.Focus) {
			return m.toggleDetailsFocus()
		}
	}

	return m.routeScreenKey(key)
}

func (m model) routeScreenKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.screen.spec().key(m, key)
}

func (m model) routeAsyncMessage(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case refreshTickMsg:
		next, viewed := m.dwellViewed(msg.at)
		next, readRuns := next.dwellRunsRead(msg.at)
		reviewing := next.refreshReviewCmd(msg.at)
		mailing := next.refreshMailCmd()
		next, edited := next.reconcileEditor()

		return next, tea.Batch(next.refreshCmd(), refreshTick(), viewed, reviewing, readRuns, mailing, edited), true
	case tea.BlurMsg:
		m.syncKeys()

		return m, nil, true
	case tea.FocusMsg:
		m.syncKeys()

		next, cmd := m.reconcileEditor()
		next.reconcileSolo()

		return next, cmd, true
	case refreshResult:
		m.applyRefresh(msg)

		return m, nil, true
	case writeResult:
		next, cmd := m.applyWrite(msg)

		return next, cmd, true
	case mailLoadedMsg:
		m.applyMailLoaded(msg)

		return m, nil, true
	case reviewLoadedMsg:
		return m, m.applyReviewLoaded(msg), true
	case reviewWroteMsg:
		m.applyReviewWrote(msg)

		return m, nil, true
	case reviewRefreshedMsg:
		m.applyReviewRefreshed(msg)

		return m, nil, true
	case roundReadyMsg:
		next, cmd := m.applyRoundReady(msg)

		return next, cmd, true
	case roundEditedMsg:
		return m, m.applyRoundEdited(msg), true
	case roundHarvestedMsg:
		m.applyRoundHarvested(msg)

		return m, nil, true
	case roundLeftoverMsg:
		m.applyRoundLeftover(msg)

		return m, nil, true
	case reviewDiffPolledMsg:
		m.applyReviewDiffPolled(msg)

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

// isTyping reports whether the panel's own bindings are off: a text input is up taking
// characters, or a key prefix is waiting on the key that completes it.
func (m model) isTyping() bool {
	typing := m.screen.spec().typing

	return typing != nil && typing(m)
}

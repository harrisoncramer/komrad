package ui

import (
	"log/slog"

	"github.com/charmbracelet/bubbles/cursor"
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

	m.holdFocusKey()

	return tea.Batch(refreshTick(), m.refreshCmd(), cursor.Blink)
}

// Update routes a message to the screen that owns it, and keeps the row cursor blinking.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.route(msg)

	routed, ok := next.(model)
	if !ok {
		return next, cmd
	}

	routed, blink := routed.updateRowCursor(msg)
	routed, warming := routed.pollWarmup()
	routed, pulse := routed.pulseMessages(m.messages.landed)
	routed = routed.releaseHiddenDetails()
	routed.holdFocusKey()

	return routed, tea.Batch(cmd, blink, warming, pulse)
}

// holdFocusKey binds the focus key while an agent's pane is on the stage, and gives it back when
// there is no pane for the key to jump to.
func (m model) holdFocusKey() {
	if m.popup == nil || m.frame == nil {
		return
	}

	m.popup.HoldFocusKey(m.pane, m.focusPane())
}

// focusPane is the agent's pane the focus key jumps to, empty where the stage holds none or the
// panel is drawn over it.
func (m model) focusPane() string {
	if m.screen != screenAgents || m.frame.IsSolo() {
		return ""
	}

	return m.frame.FocusPane()
}

func (m model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m = m.resizeTo(size)
	}

	key, isKey := msg.(tea.KeyMsg)

	if m.fatal() != nil {
		if isKey {
			return m, tea.Quit
		}

		if _, hidden := msg.(tea.BlurMsg); hidden {
			return m, tea.Quit
		}

		return m, nil
	}

	// A mark binding owns the key after it, whatever that key is otherwise bound to.
	if isKey && m.marks.pending != markPendingNone {
		return m.readMark(key)
	}

	if next, cmd, handled := m.routeAsyncMessage(msg); handled {
		return next, cmd
	}

	// Reached before the form and before a held write.
	if isKey && m.isMessagesRegion() {
		switch {
		case m.isMessagesFocused():
			return m.updateMessages(key)
		case key.String() == messagesFocusKey:
			return m.toggleMessagesFocus(), nil
		}
	}

	if isKey && m.held.active {
		// A provision can run for minutes, so esc gives the panel back rather than holding the
		// keyboard for the whole of it, with the write left in flight.
		if key.String() == "esc" {
			return m.detachForm()
		}

		return m, nil
	}

	if m.form.isActive() {
		if isKey && isBound(key.String(), formKeys().Editor) {
			if next, cmd, opened := m.openFormFieldEditor(); opened {
				return next, cmd
			}
		}

		// A resize says how big the panel's window is, and the form is drawn in a column of it, so
		// the size is rewritten here rather than in each form's own handler.
		return m.form.update(m, m.formSizeOf(msg))
	}

	if !isKey {
		return m, nil
	}

	if m.filterTarget().state.typing {
		return m.updateFilterInput(key)
	}

	// The filter has gone by here, so what is left is the screen's own input, which owns the
	// keyboard since every global key is also a character somebody means to type.
	if m.isTyping() {
		return m.routeScreenKey(key)
	}

	if next, cmd, handled := m.routeGlobalKey(key); handled {
		return next, cmd
	}

	return m.routeScreenKey(key)
}

// resizeTo takes the panel to the size a window message reports.
func (m model) resizeTo(size tea.WindowSizeMsg) model {
	//nolint:sloglint // temporary trace, no context here to carry a logger on
	slog.Default().Info("panel trace: a resize arrived", "width", size.Width, "height", size.Height, "was", m.width, "solo", m.frame.IsSolo())

	// The transcript is wrapped into the column it is read in rather than into a pane that would
	// fold it, so only a window that changed width has to wrap it again, and a preview that has
	// never drawn is one the rows are still owed to.
	rewrap := size.Width != m.width || len(m.search.preview.doc.rows) == 0

	m.height = size.Height
	m.width = size.Width
	m.awaitingSize = false

	if !m.isTabsDrawn() {
		m.blurTabs()
	}

	// A drag and a zoom both change the panel's own pane and leave the window around it alone.
	window := m.frame.WindowCols()
	if window != m.windowCols && m.windowCols > 0 {
		m.frame.RestoreSplit()
	} else {
		m.frame.SyncSplit()
	}

	m.windowCols = window

	m.sizeAgentWindows()

	// A field open in the review's sidebar is drawn in a column of this window rather than in the
	// form's, so the size is rewritten here too.
	m.review.threads.editor.resize(m.reviewInputWidth())

	if rewrap {
		m.drawPreview()
	}

	return m
}

// routeGlobalKey answers the keys the panel owns whatever screen is up, reporting whether it
// took the press so the screen underneath gets whatever it left alone.
func (m model) routeGlobalKey(key tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if isBound(key.String(), m.keys.Help.Toggle) {
		m.helpExpanded = !m.helpExpanded

		return m, nil, true
	}

	// Whether these keys act is a policy about the keys rather than the geometry, and the two do
	// not line up: the archive and the review draw the whole window and refuse them.
	if m.screen.opensInfoPane() {
		switch {
		case isBound(key.String(), m.keys.Logs.Open):
			return m.cycleLogs(), nil, true
		case isBound(key.String(), m.keys.Logs.Older):
			return m.olderLog(), nil, true
		case isBound(key.String(), m.keys.Terminal.Open):
			next, cmd := m.openTerminal()

			return next, cmd, true
		}
	}

	// The block beside the list is the panel's own to scroll, so the keys that move it are routed
	// here rather than repeated on every screen that draws one.
	if direction, ok := m.isColumnScroll(key.String()); ok {
		return m.scrollColumn(direction), nil, true
	}

	if m.isMessagesRegion() && key.String() == messagesMockKey {
		return m.pushMockMessage(), nil, true
	}

	if m.tabsSpec() != nil {
		if m.isTabsFocused() {
			next, cmd := m.updateTabs(key)

			return next, cmd, true
		}
		if isBound(key.String(), m.keys.Tabs.Focus) {
			return m.focusTabs(), nil, true
		}
	}

	if m.isDetailsScreen() {
		if m.isDetailsFocused() {
			next, cmd := m.updateDetails(key)

			return next, cmd, true
		}
		if isBound(key.String(), m.keys.Details.Focus) {
			next, cmd := m.toggleDetailsFocus()

			return next, cmd, true
		}
	}

	return m, nil, false
}

func (m model) routeScreenKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.screen.spec().key(m, key)
}

func (m model) routeAsyncMessage(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case refreshTickMsg:

		m.awaitingSize = false
		next, readRuns := m.dwellRunsRead(msg.at)
		reviewing := next.refreshReviewCmd(msg.at)
		next, edited := next.reconcileEditor()

		return next, tea.Batch(next.refreshCmd(), refreshTick(), reviewing, readRuns, edited), true
	case tea.BlurMsg:
		m.blurred = true
		m.syncKeys()
		m.keysElsewhere = !m.frame.HasKeys()

		return m, nil, true
	case tea.FocusMsg:
		m.blurred = false
		m.keysElsewhere = false
		m.syncKeys()

		next, cmd := m.reconcileEditor()
		next.reconcileSolo()

		return next, cmd, true
	case warmupTickMsg:
		next, cmd := m.applyWarmupTick()

		return next, cmd, true
	case stageTickMsg:
		cmd := m.applyStageTick(msg)

		return m, cmd, true
	case worktreeInspectedMsg:
		m.applyWorktreeInspection(msg)

		return m, nil, true
	case agentPullRequestsListedMsg:
		m.applyAgentPullRequests(msg)

		return m, nil, true
	case urlOpenedMsg:
		m.applyURLOpened(msg)

		return m, nil, true
	case refreshResult:
		cmd := m.applyRefresh(msg)

		return m, cmd, true
	case writeResult:
		next, cmd := m.applyWrite(msg)

		return next, cmd, true
	case rebasePlannedMsg:
		next, cmd := m.applyRebasePlanned(msg)

		return next, cmd, true
	case rebasedMsg:
		return m.applyRebased(msg), nil, true
	case reviewLoadedMsg:
		return m, m.applyReviewLoaded(msg), true
	case reviewPausedMsg:
		next, cmd := m.applyReviewPaused(msg)

		return next, cmd, true
	case reviewWroteMsg:
		return m, m.applyReviewWrote(msg), true
	case reviewRefreshedMsg:
		m.applyReviewRefreshed(msg)

		return m, nil, true
	case formFieldEditedMsg:
		next, cmd := m.applyFormFieldEdited(msg)

		return next, cmd, true
	case messageEditedMsg:
		m.applyMessageEdited(msg)

		return m, nil, true
	case reviewDiffPolledMsg:
		return m, m.applyReviewDiffPolled(msg), true
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
	case pulseMsg:
		cmd := m.advancePulses()

		return m, cmd, true
	case flashRevertMsg:
		m.details.flashed = 0
		m.messages.flashed = 0
		m.restageScreen()

		return m, nil, true
	default:
		return m, nil, false
	}
}

// isTyping reports whether the panel's own bindings are off: the list's filter, a text input the
// screen put up, or a key prefix waiting on the key that completes it.
func (m model) isTyping() bool {
	if m.filterTarget().state.typing {
		return true
	}

	typing := m.screen.spec().typing

	return typing != nil && typing(m)
}

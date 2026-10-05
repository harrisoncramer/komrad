package ui

import (
	"time"

	"komrad/internal/client"
	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
)

func (m *model) hide() tea.Cmd {
	session := m.session

	return func() tea.Msg {
		session.hide()
		return nil
	}
}

func (m *model) fatal() error {
	if m.configErr != nil {
		return m.configErr
	}

	return m.startupErr
}

// Init starts the tick, unless startup failed and there is nothing to keep current.
func (m *model) Init() tea.Cmd {
	if m.fatal() != nil {
		return nil
	}

	m.holdLeader()

	return tea.Batch(tick(), cursor.Blink, tea.RequestBackgroundColor)
}

// Update routes a message to the screen that owns it, and keeps the row cursor blinking.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	was := m.atStop()
	landed := m.messages.landed
	cmd := m.route(msg)
	caught := m.releaseWatch(false)

	m.settleTriage()
	blink := m.updateRowCursor(msg)
	warming := m.pollWarmup()
	spinning := m.spinLoader()
	pulse := m.pulseMessages(landed)
	m.releaseHiddenDetails()
	m.dropStaleDoc()
	if m.atStop() != was {
		m.leftStop = was
	}

	return m, tea.Batch(cmd, caught, blink, warming, spinning, pulse)
}

func (m *model) route(msg tea.Msg) tea.Cmd {
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		return m.routeWheel(wheel.X, wheel.Y, wheelTurn(wheel))
	}

	if wheel, ok := msg.(wheelMsg); ok {
		return m.routeWheel(wheel.x, wheel.y, wheel.rows)
	}

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.resizeTo(size)
	}

	key, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		m.releaseCursor()
	}

	if m.fatal() != nil {
		if isKey {
			return tea.Quit
		}

		if _, hidden := msg.(tea.BlurMsg); hidden {
			return tea.Quit
		}

		return nil
	}

	if frozen, ok := msg.(pickerFrozenMsg); ok {
		m.applyFreeze(frozen)

		return nil
	}

	if opened, ok := msg.(copyOpenedMsg); ok {
		m.applyCopyOpened(opened)

		return nil
	}

	if isKey {
		if cmd, handled := m.routeLeader(key); handled {
			return cmd
		}

		if cmd, handled := m.routePicker(key); handled {
			return cmd
		}
	}

	if landed, ok := msg.(countedWrite); ok {
		if m.landHeld(landed.countedWrite()) {
			return tea.Batch(m.closeForm(), m.route(msg))
		}
	}

	if cmd, handled := m.routeAsyncMessage(msg); handled {
		return cmd
	}

	// Reached before the form and before a held write.
	if isKey && m.canReadMessages() {
		switch {
		case m.isMessagesFocused():
			return m.updateMessages(key)
		case isBound(key.String(), m.keys.Messages.Open):
			m.toggleMessagesFocus()

			return nil
		}
	}

	if isKey && m.writes.held.active {
		// A provision can run for minutes, so esc gives the panel back rather than holding the
		// keyboard for the whole of it, with the write left in flight.
		if key.String() == "esc" {
			return m.detachForm()
		}

		return nil
	}

	if m.form.isActive() {
		if isKey && isBound(key.String(), formKeys().Editor) {
			if cmd, opened := m.openFormFieldEditor(); opened {
				return cmd
			}
		}

		// A resize says how big the panel's window is, and the form is drawn in a column of it, so
		// the size is rewritten here rather than in each form's own handler.
		return m.form.update(m, m.formSizeOf(msg))
	}

	// What is left of the messages is a screen's own results, which the screen that is up reads
	// and every other leaves alone.
	if paste, ok := msg.(tea.PasteMsg); ok && m.filter.typing {
		m.filter.input.take(paste, m.register)
		m.filter.compile()

		return nil
	}

	if !isKey {
		return m.routeScreenMessage(msg)
	}

	// The switcher answers its own keys through every view it opens, one that types included.
	if m.isViewsOpen() {
		if m.holdsViewsKey(key.String()) {
			return m.updateViews(key)
		}

		m.closeViews()
	}

	if m.filter.typing {
		return m.updateFilterInput(key)
	}

	if m.reading.filter.typing && m.isDocReadable() {
		return m.updateDocFilter(key)
	}

	// The filter has gone by here, so what is left is the screen's own input, which owns the
	// keyboard since every global key is also a character somebody means to type.
	if m.isTyping() {
		return m.routeScreenMessage(key)
	}

	if cmd, handled := m.routeGlobalKey(key); handled {
		return cmd
	}

	return m.routeScreenMessage(key)
}

// sidebarStep is how many columns one press of a sidebar key moves its edge.
const sidebarStep = 4

// resizeTo takes the panel to the size a window message reports.
func (m *model) resizeTo(size tea.WindowSizeMsg) {
	// The transcript is wrapped into the column it is read in rather than into a pane that would
	// fold it, so only a window that changed width has to wrap it again, and a preview that has
	// never drawn is one the rows are still owed to.
	width := size.Width - m.countInsetCols()
	rewrap := width != m.term.width || len(m.search.preview.doc.rows) == 0

	m.term.height = max(size.Height, 0)
	m.term.width = max(width, 0)
	m.term.paneCols = size.Width
	m.term.awaitingSince = time.Time{}

	if !m.isTabsDrawn() {
		m.blurTabs()
	}
	if !m.isKindsDrawn() {
		m.blurKinds()
	}

	// A drag and a zoom both change the panel's own pane and leave the window around it alone.
	window := m.frame.WindowCols()
	if window != m.term.windowCols && m.term.windowCols > 0 {
		m.frame.RestoreSplit()
	} else {
		m.frame.SyncSplit()
	}

	m.term.windowCols = window

	m.relayout(rewrap)
}

// applySizeRead settles a size the frame read back, leaving a changed one to the resize the
// terminal itself reports.
func (m *model) applySizeRead(msg sizeReadMsg) tea.Cmd {
	if msg.size.Width == m.term.paneCols && msg.size.Height == m.term.height {
		m.term.awaitingSince = time.Time{}

		if width := max(m.term.paneCols-m.countInsetCols(), 0); width != m.term.width {
			m.term.width = width
			m.relayout(true)
		}

		return nil
	}

	return startTimer(sizeFallback, func(time.Time) tea.Msg {
		return sizeFallbackMsg(msg)
	})
}

// applySizeFallback asks the terminal for its size when a read-back size was never reported.
func (m *model) applySizeFallback(msg sizeFallbackMsg) tea.Cmd {
	if msg.size.Width == m.term.paneCols && msg.size.Height == m.term.height {
		return nil
	}

	return tea.RequestWindowSize
}

// relayout sizes everything drawn off the split again, once the split has moved.
func (m *model) relayout(rewrap bool) {
	m.sizeAgentWindows()

	// A field open in the review's sidebar is drawn in a column of this window rather than in the
	// form's, so the size is rewritten here too.
	m.review.threads.editor.resize(reviewInputWidth(m.review.measurePopupInner(m)))

	if rewrap {
		m.search.drawPreview(m)
	}
}

// nudgeSidebar moves the sidebar's edge by a number of columns.
func (m *model) nudgeSidebar(delta int) {
	m.frame.Nudge(delta)
	m.relayout(true)
}

// routeGlobalKey answers the keys the panel owns whatever screen is up, reporting whether it
// took the press so the screen underneath gets whatever it left alone.
func (m *model) routeGlobalKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if drawsSwitcher(m.screen) && !m.isTabsFocused() && !m.isKindsFocused() {
		if delta := m.viewsDelta(key.String()); delta != 0 {
			return m.openViews(delta), true
		}
	}

	if isBound(key.String(), m.keys.Help.Toggle) {
		m.helpExpanded = !m.helpExpanded

		return nil, true
	}

	switch {
	case isBound(key.String(), m.keys.Sidebar.Narrow):
		m.nudgeSidebar(-sidebarStep)

		return nil, true
	case isBound(key.String(), m.keys.Sidebar.Widen):
		m.nudgeSidebar(sidebarStep)

		return nil, true
	}

	if m.isDetailsScreen() && m.canReadMessages() && key.String() == messagesMockKey {
		m.pushMockMessage()

		return nil, true
	}

	if _, ok := m.activeTabs(); ok {
		if m.isTabsFocused() {
			return m.updateTabs(key), true
		}
		if isBound(key.String(), m.keys.Tabs.Focus) {
			m.focusTabs()

			return nil, true
		}
	}

	if _, ok := m.activeKinds(); ok {
		if m.isKindsFocused() {
			return m.updateKinds(key), true
		}
		if isBound(key.String(), m.keys.Kinds.Focus) {
			m.focusKinds()

			return nil, true
		}
	}

	if isBound(key.String(), m.keys.Menu.Open) {
		return m.openMenu(), true
	}

	if m.isDetailsScreen() && m.isDetailsFocused() {
		return m.updateDetails(key), true
	}

	if m.isDocFocused() {
		return m.updateDoc(key), true
	}

	return nil, false
}

// routeScreenMessage hands a message to the screen that is up.
func (m *model) routeScreenMessage(msg tea.Msg) tea.Cmd {
	return m.active().update(m, msg)
}

func (m *model) routeAsyncMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.isAwaitingSize() && msg.at.Sub(m.term.awaitingSince) >= sizeFallback {
			m.term.awaitingSince = time.Time{}
		}

		edited := m.reconcileEditor()

		m.noticeRebuild()
		if m.fatal() != nil {
			return nil, true
		}

		return tea.Batch(tick(), edited), true
	case engineBuildMsg:
		m.landEngineBuild(msg)

		return nil, true
	case tea.BackgroundColorMsg:
		m.term.isDark = msg.IsDark()
		paint.SetDarkBackground(m.term.isDark)

		return nil, true
	case tea.BlurMsg:
		m.term.blurred = true
		m.syncKeys()
		m.term.keysElsewhere = !m.frame.HasKeys()

		return nil, true
	case tea.FocusMsg:
		m.term.blurred = false
		m.term.paneCopying = false
		m.term.keysElsewhere = false
		m.syncKeys()

		cmd := m.reconcileEditor()
		m.reconcileSolo()

		return cmd, true
	case sizeReadMsg:
		return m.applySizeRead(msg), true
	case sizeFallbackMsg:
		return m.applySizeFallback(msg), true
	case warmupTickMsg:
		return m.applyWarmupTick(), true
	case stageTickMsg:
		return m.applyStageTick(msg), true
	case focusGuestMsg:
		m.selectGuest(msg.pane)

		return nil, true
	case lastScreenMsg:
		m.applyLastScreen(msg)

		return nil, true
	case worktreeInspectedMsg:
		m.applyWorktreeInspection(msg)

		return nil, true
	case documentsListedMsg:
		return m.applyDocumentsListed(msg), true
	case statusChecksListedMsg:
		return tea.Batch(m.applyStatusChecksListed(msg), m.statusCheckLogs.readsCmd(m)), true
	case statusCheckLogReadMsg:
		return m.applyStatusCheckLogRead(msg), true
	case documentReadMsg:
		m.applyDocumentRead(msg)

		return nil, true
	case logsSourcesMsg:
		return m.applyLogsSources(msg), true
	case logsReadMsg:
		return m.applyLogsRead(msg), true
	case logsTickMsg:
		return m.applyLogsTick(msg), true
	case logsSettledMsg:
		return m.applyLogsSettled(msg), true
	case documentVersionsListedMsg:
		return m.applyDocumentVersionsListed(msg), true
	case documentVersionReadMsg:
		m.applyDocumentVersionRead(msg)

		return nil, true
	case urlOpenedMsg:
		m.applyURLOpened(msg)

		return nil, true
	case client.WatchBatch:
		return m.applyWatch(msg), true
	case watchLostMsg:
		m.applyWatchLost(msg)

		return nil, true
	case writeResult:
		return m.applyWrite(msg), true
	case reviewPausedMsg:
		if m.isPausing(msg.agentID) {
			return nil, false
		}

		return m.releaseAbandonedPause(msg), true
	case gitPlannedMsg:
		return m.applyGitPlanned(msg), true
	case gitDoneMsg:
		return m.applyGitDone(msg), true
	case draftedMsg:
		return m.applyDrafted(msg), true
	case rereviewedMsg:
		return m.applyRereviewed(msg), true
	case autoMergedMsg:
		return m.applyAutoMerged(msg), true
	case formFieldEditedMsg:
		return m.applyFormFieldEdited(msg), true
	case copyResult:
		m.applyCopy(msg)

		return nil, true
	case pulseMsg:
		return m.advancePulses(), true
	case loaderTickMsg:
		return m.advanceLoader(), true
	case messagesFlashDoneMsg:
		m.endMessagesFlash(msg)

		return nil, true
	case flashRevertMsg:
		m.details.flashed = 0
		m.messages.flashed = 0
		m.restageScreen()

		return nil, true
	default:
		return nil, false
	}
}

// reconcileEditor catches the info pane, the scratch editor and a screen's editor going away on
// their own.
func (m *model) reconcileEditor() tea.Cmd {
	m.reconcileTerminal()

	if cmd := m.reconcileScratch(); cmd != nil {
		return cmd
	}

	if screen, ok := m.active().(editorReconciler); ok {
		return screen.reconcileEditor(m)
	}

	return nil
}

// isTyping reports whether the panel's own bindings are off: the list's filter, the document's
// search, a text input the screen put up, or a key prefix waiting on the key that completes it.
func (m *model) isTyping() bool {
	if m.filter.typing || (m.reading.filter.typing && m.isDocReadable()) {
		return true
	}

	return m.active().takesEveryKey()
}

// columnWheelStep is how many rows one turn of the wheel moves the block beside the list.
const columnWheelStep = 3

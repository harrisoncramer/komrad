package ui

import (
	"time"

	"komrad/internal/client"
	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
)

func (m model) hide() tea.Cmd {
	return func() tea.Msg {
		m.session.Hide()
		return nil
	}
}

func (m model) fatal() error {
	if m.configErr != nil {
		return m.configErr
	}

	return m.startupErr
}

// Init starts the tick, unless startup failed and there is nothing to keep current.
func (m model) Init() tea.Cmd {
	if m.fatal() != nil {
		return nil
	}

	m.holdCycleKeys()

	return tea.Batch(tick(), cursor.Blink, tea.RequestBackgroundColor)
}

// Update routes a message to the screen that owns it, and keeps the row cursor blinking.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.route(msg)

	routed, ok := next.(model)
	if !ok {
		return next, cmd
	}

	routed, blink := routed.updateRowCursor(msg)
	warming := routed.warm.poll()
	spinning := routed.spinLoader()
	routed, pulse := routed.pulseMessages(m.messages.landed)
	routed = routed.releaseHiddenDetails()
	routed = routed.dropStaleDocScroll()
	routed.holdCycleKeys()

	return routed, tea.Batch(cmd, blink, warming, spinning, pulse)
}

// focusPane is the agent's pane the cycle stops at, empty where the stage holds none or the
// panel is drawn over it.
func (m model) focusPane() string {
	if m.screen != screenAgents || m.frame.IsSolo() {
		return ""
	}

	return m.frame.FocusPane()
}

func (m model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		return m.routeWheel(wheel)
	}

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m = m.resizeTo(size)
	}

	key, isKey := msg.(tea.KeyPressMsg)
	if isKey {
		m.landing = landing{}
	}

	if m.fatal() != nil {
		if isKey {
			return m, tea.Quit
		}

		if _, hidden := msg.(tea.BlurMsg); hidden {
			return m, tea.Quit
		}

		return m, nil
	}

	// A cycle key pressed in another pane is answered ahead of every mode.
	if isKey {
		if from, ok := m.findPressedElsewhere(key.String()); ok {
			return m.cycleFromElsewhere(key.String(), from)
		}
	}

	if next, cmd, handled := m.routeAsyncMessage(msg); handled {
		return next, cmd
	}

	// Reached before the form and before a held write.
	if isKey && m.canReadMessages() {
		switch {
		case m.isMessagesFocused() && m.isCycleKey(key.String()):
			return m.toggleMessagesFocus(), nil
		case m.isMessagesFocused():
			return m.updateMessages(key)
		case isBound(key.String(), m.keys.Messages.Open):
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

	// What is left of the messages is a screen's own results, which the screen that is up reads
	// and every other leaves alone.
	if paste, ok := msg.(tea.PasteMsg); ok && m.filter.typing {
		m.filter.setQuery(m.filter.query + pastedLine(paste))

		return m, nil
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

	// The filter has gone by here, so what is left is the screen's own input, which owns the
	// keyboard since every global key is also a character somebody means to type.
	if m.isTyping() {
		return m.routeScreenMessage(key)
	}

	if next, cmd, handled := m.routeGlobalKey(key); handled {
		return next, cmd
	}

	return m.routeScreenMessage(key)
}

// sidebarStep is how many columns one press of a sidebar key moves its edge.
const sidebarStep = 4

// resizeTo takes the panel to the size a window message reports.
func (m model) resizeTo(size tea.WindowSizeMsg) model {
	// The transcript is wrapped into the column it is read in rather than into a pane that would
	// fold it, so only a window that changed width has to wrap it again, and a preview that has
	// never drawn is one the rows are still owed to.
	width := size.Width - m.countInsetCols()
	rewrap := width != m.width || len(m.search.preview.doc.rows) == 0

	m.height = max(size.Height, 0)
	m.width = max(width, 0)
	m.paneCols = size.Width
	m.awaitingSince = time.Time{}

	if !m.isTabsDrawn() {
		m.blurTabs()
	}
	if !m.isKindsDrawn() {
		m.blurKinds()
	}

	// A drag and a zoom both change the panel's own pane and leave the window around it alone.
	window := m.frame.WindowCols()
	if window != m.windowCols && m.windowCols > 0 {
		m.frame.RestoreSplit()
	} else {
		m.frame.SyncSplit()
	}

	m.windowCols = window

	return m.relayout(rewrap)
}

// applySizeRead settles a size the frame read back, leaving a changed one to the resize the
// terminal itself reports.
func (m *model) applySizeRead(msg sizeReadMsg) tea.Cmd {
	if msg.size.Width == m.paneCols && msg.size.Height == m.height {
		m.awaitingSince = time.Time{}

		if width := max(m.paneCols-m.countInsetCols(), 0); width != m.width {
			m.width = width
			*m = m.relayout(true)
		}

		return nil
	}

	return startTimer(sizeFallback, func(time.Time) tea.Msg {
		return sizeFallbackMsg(msg)
	})
}

// applySizeFallback hands the renderer a read-back size the terminal never reported.
func (m model) applySizeFallback(msg sizeFallbackMsg) tea.Cmd {
	if msg.size.Width == m.paneCols && msg.size.Height == m.height {
		return nil
	}

	return func() tea.Msg {
		return msg.size
	}
}

// relayout sizes everything drawn off the split again, once the split has moved.
func (m model) relayout(rewrap bool) model {
	m.sizeAgentWindows()

	// A field open in the review's sidebar is drawn in a column of this window rather than in the
	// form's, so the size is rewritten here too.
	m.review.threads.editor.resize(reviewInputWidth(m.paneWidth()))

	if rewrap {
		m.search.drawPreview(m.shared())
	}

	return m
}

// nudgeSidebar moves the sidebar's edge by a number of columns.
func (m model) nudgeSidebar(delta int) model {
	m.frame.Nudge(delta)

	return m.relayout(true)
}

// routeGlobalKey answers the keys the panel owns whatever screen is up, reporting whether it
// took the press so the screen underneath gets whatever it left alone.
func (m model) routeGlobalKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.screen.picksViews() && !m.isTabsFocused() && !m.isKindsFocused() {
		if delta := m.viewsDelta(key.String()); delta != 0 {
			next, cmd := m.openViews(delta)

			return next, cmd, true
		}
	}

	if isBound(key.String(), m.keys.Help.Toggle) {
		m.helpExpanded = !m.helpExpanded

		return m, nil, true
	}

	switch {
	case isBound(key.String(), m.keys.Sidebar.Narrow):
		return m.nudgeSidebar(-sidebarStep), nil, true
	case isBound(key.String(), m.keys.Sidebar.Widen):
		return m.nudgeSidebar(sidebarStep), nil, true
	}

	if m.isDetailsScreen() && m.canReadMessages() && key.String() == messagesMockKey {
		return m.pushMockMessage(), nil, true
	}

	if _, ok := m.active().(blocked); (ok || m.isDetailsScreen()) && m.isCycleKey(key.String()) {
		next, cmd := m.cycleView(key.String(), m.readSidebarStop())

		return next, cmd, true
	}

	if _, ok := m.activeTabs(); ok {
		if m.isTabsFocused() {
			next, cmd := m.updateTabs(key)

			return next, cmd, true
		}
		if isBound(key.String(), m.keys.Tabs.Focus) {
			return m.focusTabs(), nil, true
		}
	}

	if _, ok := m.activeKinds(); ok {
		if m.isKindsFocused() {
			next, cmd := m.updateKinds(key)

			return next, cmd, true
		}
		if isBound(key.String(), m.keys.Kinds.Focus) {
			return m.focusKinds(), nil, true
		}
	}

	if isBound(key.String(), m.keys.Menu.Open) {
		cmd := m.openMenu()

		return m, cmd, true
	}

	if m.isDetailsScreen() && m.isDetailsFocused() {
		next, cmd := m.updateDetails(key)

		return next, cmd, true
	}

	return m, nil, false
}

// routeScreenMessage hands a message to the screen that is up.
func (m model) routeScreenMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.active().update(&m, m.shared(), msg)

	return m, cmd
}

func (m model) routeAsyncMessage(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.isAwaitingSize() && msg.at.Sub(m.awaitingSince) >= sizeFallback {
			m.awaitingSince = time.Time{}
		}

		next := m
		edited := next.reconcileEditor()

		next = next.noticeRebuild()
		if next.fatal() != nil {
			return next, nil, true
		}

		return next, tea.Batch(tick(), edited), true
	case engineBuildMsg:
		return m.landEngineBuild(msg), nil, true
	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		paint.SetDarkBackground(m.isDark)

		return m, nil, true
	case tea.BlurMsg:
		m.blurred = true
		m.syncKeys()
		m.keysElsewhere = !m.frame.HasKeys()

		return m, nil, true
	case tea.FocusMsg:
		m.blurred = false
		m.keysElsewhere = false
		m.syncKeys()

		cmd := m.reconcileEditor()
		m.reconcileSolo()

		return m, cmd, true
	case sizeReadMsg:
		return m, m.applySizeRead(msg), true
	case sizeFallbackMsg:
		return m, m.applySizeFallback(msg), true
	case warmupTickMsg:
		next, cmd := m.applyWarmupTick()

		return next, cmd, true
	case stageTickMsg:
		cmd := m.applyStageTick(msg)

		return m, cmd, true
	case conversationPreviewMsg:
		m.applyConversationPreview(msg)

		return m, nil, true
	case worktreeInspectedMsg:
		m.applyWorktreeInspection(msg)

		return m, nil, true
	case documentsListedMsg:
		cmd := m.applyDocumentsListed(msg)

		return m, cmd, true
	case documentReadMsg:
		m.applyDocumentRead(msg)

		return m, nil, true
	case logsSourcesMsg:
		cmd := m.applyLogsSources(msg)

		return m, cmd, true
	case logsReadMsg:
		cmd := m.applyLogsRead(msg)

		return m, cmd, true
	case logsTickMsg:
		cmd := m.applyLogsTick(msg)

		return m, cmd, true
	case logsSettledMsg:
		cmd := m.applyLogsSettled(msg)

		return m, cmd, true
	case documentVersionsListedMsg:
		cmd := m.applyDocumentVersionsListed(msg)

		return m, cmd, true
	case documentVersionReadMsg:
		m.applyDocumentVersionRead(msg)

		return m, nil, true
	case urlOpenedMsg:
		m.applyURLOpened(msg)

		return m, nil, true
	case client.WatchBatch:
		cmd := m.applyWatch(msg)

		return m, cmd, true
	case watchLostMsg:
		m.applyWatchLost(msg)

		return m, nil, true
	case writeResult:
		next, cmd := m.applyWrite(msg)

		return next, cmd, true
	case reviewPausedMsg:
		if m.isPassing(msg.agentID) {
			return m, nil, false
		}

		return m, m.releaseAbandonedPause(msg), true
	case rebasePlannedMsg:
		next, cmd := m.applyRebasePlanned(msg)

		return next, cmd, true
	case rebasedMsg:
		next, cmd := m.applyRebased(msg)

		return next, cmd, true
	case mergedMsg:
		next, cmd := m.applyMerged(msg)

		return next, cmd, true
	case draftedMsg:
		next, cmd := m.applyDrafted(msg)

		return next, cmd, true
	case formFieldEditedMsg:
		next, cmd := m.applyFormFieldEdited(msg)

		return next, cmd, true
	case copyResult:
		m.applyCopy(msg)

		return m, nil, true
	case pulseMsg:
		cmd := m.advancePulses()

		return m, cmd, true
	case loaderTickMsg:
		cmd := m.advanceLoader()

		return m, cmd, true
	case messagesFlashDoneMsg:
		return m.endMessagesFlash(msg), nil, true
	case flashRevertMsg:
		m.details.flashed = 0
		m.messages.flashed = 0
		m.restageScreen()

		return m, nil, true
	default:
		return m, nil, false
	}
}

// reconcileEditor catches the info pane, the scratch editor and a screen's editor going away on
// their own.
func (m *model) reconcileEditor() tea.Cmd {
	m.reconcileInfo()

	if cmd := m.reconcileScratch(); cmd != nil {
		return cmd
	}

	switch m.screen {
	case screenReview:
		return m.review.reconcileEditor(m, m.shared())
	case screenDocuments:
		return m.documents.reconcileEditor(m)
	case screenAgents:
		return m.agents.reconcileEditor(m)
	case screenSchedules, screenArchive, screenSearch, screenWorktrees, screenLogs:
	}

	return nil
}

// isTyping reports whether the panel's own bindings are off: the list's filter, a text input the
// screen put up, or a key prefix waiting on the key that completes it.
func (m model) isTyping() bool {
	if m.filter.typing {
		return true
	}

	return m.active().takesEveryKey()
}

// columnScroller is a screen that scrolls its own block beside the list while it says it does.
type columnScroller interface {
	doesScrollColumn(s panelShared) bool
	scrollColumn(h host, s panelShared, rows int) tea.Cmd
}

// listScroller is a screen whose scroll key does not move its list.
type listScroller interface {
	scrollList(h host, s panelShared, rows int) tea.Cmd
}

// columnWheelStep is how many rows one turn of the wheel moves the block beside the list.
const columnWheelStep = 3

// routeWheel scrolls the block beside the list when the mouse is over it, and the list otherwise.
func (m model) routeWheel(wheel tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	pressed, ok := m.wheelKey(wheel)
	if !ok {
		return m, nil
	}

	rows := 1
	if wheel.Button == tea.MouseWheelUp {
		rows = -1
	}

	if m.form.isActive() || m.held.active || !m.isOverColumn(wheel.X) {
		if screen, ok := m.active().(listScroller); ok {
			return m, screen.scrollList(&m, m.shared(), rows)
		}

		return m.route(pressed)
	}

	if screen, ok := m.active().(columnScroller); ok && screen.doesScrollColumn(m.shared()) {
		return m, screen.scrollColumn(&m, m.shared(), rows*columnWheelStep)
	}

	m.scrollDoc(rows * columnWheelStep)

	return m, nil
}

// wheelKey is the scroll key a turn of the mouse wheel presses.
func (m model) wheelKey(wheel tea.MouseWheelMsg) (tea.KeyPressMsg, bool) {
	var keys []string

	switch wheel.Button {
	case tea.MouseWheelUp:
		keys = m.keys.Column.ScrollUp
	case tea.MouseWheelDown:
		keys = m.keys.Column.ScrollDown
	default:
		return tea.KeyPressMsg{}, false
	}

	if len(keys) == 0 {
		return tea.KeyPressMsg{}, false
	}

	return keyMsg(keys[0]), true
}

package ui

import (
	"time"

	"komrad/internal/client"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"

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

// Init starts the watch and the tick, unless startup failed and there is nothing to keep current.
func (m *model) Init() tea.Cmd {
	if m.fatal() != nil {
		return nil
	}

	if m.isGated() {
		return m.checkAccessCmd()
	}

	m.startWatch()
	m.holdLeader()

	return tea.Batch(m.tick(), tea.RequestBackgroundColor)
}

// Update routes a message to the screen that owns it.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(wheelHeldMsg); ok {
		return m, m.clock.After(wheelFrame, func(time.Time) tea.Msg { return wheelFlushMsg{} })
	}

	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.wheelHit = wheelHit{}
	}
	m.dropFrames()
	was := m.atStop()
	landed := m.messages.landed
	cmd := m.route(msg)
	caught := m.releaseWatch(false)

	m.settleTriage()
	warming := m.pollWarmup()
	spinning := m.spinLoader()
	pulse := m.pulseMessages(landed)
	m.forgetDetailsFit()
	m.releaseHiddenDetails()
	m.dropStaleDoc()
	said := m.readSaidCmd()
	if m.layoutSaid() {
		m.openSaid()
	}
	if m.atStop() != was {
		m.leftStop = was
	}
	m.buildFrame()
	m.dropRenderedView()

	return m, tea.Batch(cmd, caught, warming, spinning, pulse, said)
}

func (m *model) route(msg tea.Msg) tea.Cmd {
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		return m.routeWheel(wheel.X, wheel.Y, wheelTurn(wheel))
	}

	if wheel, ok := msg.(wheelMsg); ok {
		return m.routeWheel(wheel.x, wheel.y, wheel.rows)
	}

	if click, ok := msg.(tea.MouseClickMsg); ok {
		return m.routeClick(click)
	}

	if size, ok := msg.(tea.WindowSizeMsg); ok {
		if m.frame.IsStaleSize(size.Width, size.Height) {
			return nil
		}

		m.resizeTo(size)
		m.wheelHit = wheelHit{}
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

	if m.isGated() {
		return m.routeSignIn(msg)
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

	if cmd, handled := m.routeAsyncMessage(msg); handled {
		return cmd
	}

	if !isKey {
		if cmd, owned := m.routeResult(msg); owned {
			return cmd
		}
	}

	// Reached before the form and before a held write.
	if isKey && m.canReadMessages() {
		switch {
		case m.isMessagesFocused():
			return m.updateMessages(key)
		case keys.IsBound(key.String(), m.keys.Messages.Open):
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
		if isKey && keys.IsBound(key.String(), fixedKeys().Editor) {
			if cmd, opened := m.openFormFieldEditor(); opened {
				return cmd
			}
		}

		// A resize says how big the panel's window is, and the form is rendered in a column of it, so
		// the size is rewritten here rather than in each form's own handler.
		return m.form.update(m, m.formSizeOf(msg))
	}

	// The messages left over are a screen's own results, which the screen that is up reads and every
	// other screen leaves alone.
	if paste, ok := msg.(tea.PasteMsg); ok && m.filter.typing {
		m.filter.input.Take(paste, m.register)
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
	width := size.Width - m.countInsetCols()

	m.term.height = max(size.Height, 0)
	m.term.width = max(width, 0)
	m.term.paneCols = size.Width
	m.term.awaitingSince = time.Time{}

	if !m.isTabsRendered() {
		m.blurTabs()
	}
	if !m.isKindsRendered() {
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

	m.relayout()
	m.restageBeside()
}

// applySizeRead settles a size the frame read back, leaving a changed one to the resize the
// terminal itself reports.
func (m *model) applySizeRead(msg sizeReadMsg) tea.Cmd {
	if m.frame.IsStaleSize(msg.size.Width, msg.size.Height) {
		return nil
	}

	if msg.size.Width == m.term.paneCols && msg.size.Height == m.term.height {
		m.term.awaitingSince = time.Time{}

		if width := max(m.term.paneCols-m.countInsetCols(), 0); width != m.term.width {
			m.term.width = width
			m.relayout()
		}

		return nil
	}

	return m.clock.After(sizeFallback, func(time.Time) tea.Msg {
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

// relayout sizes everything rendered off the split again, once the split has moved.
func (m *model) relayout() {
	m.sizeAgentWindows()

	// A field open in the review's sidebar is rendered in a column of this window rather than in the
	// form's, so the size is rewritten here too.
	m.review.threads.editor.resize(reviewInputWidth(m.review.measurePopupInner(m)))
}

// nudgeSidebar moves the sidebar's edge by a number of columns.
func (m *model) nudgeSidebar(delta int) {
	m.frame.Nudge(delta)
	m.relayout()
}

// routeScreenMessage hands a message to the screen that is up.
func (m *model) routeScreenMessage(msg tea.Msg) tea.Cmd {
	return m.apply(m.active().update(m, msg))
}

// routeResult hands a message to every screen, and reports whether any of them owned it.
func (m *model) routeResult(msg tea.Msg) (tea.Cmd, bool) {
	var cmds []tea.Cmd
	owned := false
	for _, s := range m.screens() {
		request, ok := s.receive(m, msg)
		if !ok {
			continue
		}

		owned = true
		cmds = append(cmds, m.apply(request))
	}

	return tea.Batch(cmds...), owned
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

		return tea.Batch(m.tick(), edited), true
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

		return m.rerenderWorktreeConfirm(msg.dir), true
	case saysSettledMsg:
		return m.applySaysSettled(msg), true
	case saysFoundMsg:
		m.applySaysFound(msg)

		return nil, true
	case saidTranscriptMsg:
		m.applySaidTranscript(msg)

		return nil, true
	case docFilterSettledMsg:
		m.applyDocFilterSettled(msg)

		return nil, true
	case urlOpenedMsg:
		return m.apply(m.applyURLOpened(msg)), true
	case client.WatchBatch:
		return m.applyWatch(msg), true
	case watchLostMsg:
		return m.applyWatchLost(msg), true
	case accessCheckedMsg:
		m.applyAccessRechecked(msg.err)

		return nil, true
	case writeResult:
		return m.applyWrite(msg), true
	case reviewPausedMsg:
		if m.isPausing(msg.agentID) {
			return nil, false
		}

		return m.apply(m.releaseAbandonedPause(msg)), true
	case gitPlannedMsg:
		return m.apply(m.applyGitPlanned(msg)), true
	case gitDoneMsg:
		return m.apply(m.applyGitDone(msg)), true
	case mergePlannedMsg:
		return m.apply(m.applyMergePlanned(msg)), true
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

// reconcileEditor catches the info pane and a screen's editor going away on their own.
func (m *model) reconcileEditor() tea.Cmd {
	m.reconcileTerminal()

	if screen, ok := m.active().(editorReconciler); ok {
		return m.apply(screen.reconcileEditor(m))
	}

	return nil
}

// isTyping reports whether the panel's own bindings are off: the list's filter, the document's
// search, a text input the screen put up, or a key prefix waiting on the key that completes it.
func (m *model) isTyping() bool {
	return m.isSearchTyping() || m.active().takesEveryKey()
}

func (m *model) isSearchTyping() bool {
	if m.filter.typing || (m.reading.filter.typing && m.isDocReadable()) {
		return true
	}

	s, ok := m.active().(searchTyper)

	return ok && s.isSearchTyping()
}

// columnWheelStep is how many rows one turn of the wheel moves the block beside the list.
const columnWheelStep = 3

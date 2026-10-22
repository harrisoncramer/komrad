package ui

import (
	"komrad/internal/ui/column"
	"komrad/internal/ui/field"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/layout"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// opensTerminal reports whether the key that opens a shell beside the panel acts on a screen.
func opensTerminal(s screen.ID) bool {
	switch s {
	case screen.Agents, screen.Schedules:
		return true
	case screen.Review, screen.Worktrees, screen.Documents, screen.Logs, screen.StatusChecks, screen.StatusCheckLogs:
		return false
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return false
}

// screenModel is what the panel asks of whichever screen is up.
type screenModel interface {
	// update takes a message the panel's own bindings did not claim.
	update(m *model, in screen.Inputs, msg tea.Msg) screen.Request

	// receive takes a result the screen may have asked for, whether it is up or not, and reports
	// whether the result was the screen's own.
	receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool)

	// doesRenderFromListing reports whether the screen renders the watch's listings.
	doesRenderFromListing() bool

	// enter lands the cursor on the target where the target is on screen, and puts the stage
	// right, once the root has put the screen up.
	enter(m *model, in screen.Inputs, target screen.Target) screen.Request

	// takesEveryKey reports whether something on the screen is taking every key.
	takesEveryKey() bool

	// column is the block the screen renders beside its own list.
	column(m *model, in screen.Inputs) column.Spec

	// stageResting stages the row the cursor came to rest on and asks for what the screen reads of it.
	stageResting(m *model, in screen.Inputs) screen.Request

	// dispatches is the tables the screen dispatches through, one at a time, walked behind the
	// global keys.
	dispatches(m *model) []keys.Table
}

// recordRenderer is a screen that renders the hovered row's record under its list.
type recordRenderer interface {
	// details is the record the cursor is standing on.
	details(m *model, in screen.Inputs) details
}

// listRenderer is a screen the root renders as a list from the params it answers.
type listRenderer interface {
	listParams(m *model, in screen.Inputs) list.Params
}

// chromeRenderer is a screen that draws itself under the chrome the root hands it.
type chromeRenderer interface {
	view(m *model, in screen.Inputs, chrome screen.Chrome) string
}

// activeLister is a screen whose list the user can narrow by typing a pattern.
type activeLister interface {
	activeList() keyedList
}

type keyedList interface {
	// hovered is the key of the row the cursor is on, read before the pattern changes.
	hovered(m *model, in screen.Inputs) string

	// place puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	place(m *model, in screen.Inputs, key string) screen.Request

	// filterRows is every row on screen with the text a search reads, in the order they are
	// rendered.
	filterRows(m *model, in screen.Inputs) []list.Row

	click(m *model, in screen.Inputs, key string, nth int) screen.Request
}

// kindSwitcher is a screen that lists more than one kind of resource and switches between them
// on a row of its own.
type kindSwitcher interface {
	kinds(m *model) stops

	// kindsFocused is whether the row of kinds holds the keys.
	kindsFocused(m *model) *bool

	// stepKind moves the list delta kinds along, wrapping at both ends.
	stepKind(m *model, in screen.Inputs, delta int) screen.Request

	// kindsSwitch is whether the kind listed may change.
	kindsSwitch(m *model) bool
}

// tabSwitcher is a screen with a strip of tabs over its list.
type tabSwitcher interface {
	tabs() tabStrip

	// tabsState is where the screen keeps the tab chosen and whether the strip holds the keys.
	tabsState() *tabsState
}

// columnFocuser is a screen whose block beside the list can take the keys and be scrolled.
type columnFocuser interface {
	// isBlockRendered is whether the block is rendered with something in it to take the keys.
	isBlockRendered(m *model, in screen.Inputs) bool

	// isBlockFocused is whether the block holds the keys.
	isBlockFocused(m *model, in screen.Inputs) bool

	focusBlock(m *model)
	blurBlock()
	doesScrollColumn(m *model, in screen.Inputs) bool
	scrollColumn(m *model, rows int) screen.Request
}

// crumbRenderer is a screen opened from another that renders the trail back to it.
type crumbRenderer interface {
	// parent is the view that opened the page.
	parent() screen.ID

	// crumbs is the trail under the parent, down to the page.
	crumbs(m *model, in screen.Inputs) crumbs
}

// hoveredAgentReporter is a screen whose cursor can rest on an agent's row.
type hoveredAgentReporter interface {
	hoveredAgentID(m *model, in screen.Inputs) string
	hoveredRow(m *model, in screen.Inputs) (agentRow, bool)
}

// savedScreenRenderer is a screen that renders a stopped agent's saved screen beside its list.
type savedScreenRenderer interface {
	screenedAgentID(m *model, in screen.Inputs) (string, bool)
	screenNotice(m *model, in screen.Inputs) string
}

// screenLeaver is a screen that puts down what it had open when another screen goes up.
type screenLeaver interface {
	leaveScreen(m *model) screen.Request
}

// rowStager is a screen that puts its hovered row on the stage.
type rowStager interface {
	stage(m *model, in screen.Inputs) screen.Request
}

// hoveredNamer is a screen that names its hovered row at the end of the trail.
type hoveredNamer interface {
	hoveredName(m *model, in screen.Inputs) string
}

// screenMenuBuilder is a screen that adds its own band to the menu.
type screenMenuBuilder interface {
	buildScreenMenu(m *model, in screen.Inputs) (menu.Menu, bool)
}

// itemMenuBuilder is a screen that adds a band to the menu for its hovered row.
type itemMenuBuilder interface {
	buildItemMenu(m *model, in screen.Inputs) (menu.Menu, bool)
}

// lineTyper is a screen with a one-row field the user can type into.
type lineTyper interface {
	typedLine() (field.Line, bool)
}

type searchTyper interface {
	isSearchTyping() bool
}

// listScroller is a screen whose scroll key does something other than move its list.
type listScroller interface {
	scrollList(m *model, rows int) screen.Request
}

// loadingReporter is a screen that can be waiting on the server for what it renders.
type loadingReporter interface {
	isLoading(m *model, in screen.Inputs) bool
}

// renamer is a screen that can rename its hovered row in place.
type renamer interface {
	isRenaming() bool
}

// editorReconciler is a screen that keeps an editor it opened on the stage in step with its
// cursor.
type editorReconciler interface {
	reconcileEditor(m *model, in screen.Inputs) screen.Request
}

// agentPauser is a screen that can pause an agent while it is up.
type agentPauser interface {
	isPausing(agentID string) bool
}

// active is the screen that is up.
func (m *model) active() screenModel {
	return m.screenOf(m.screen)
}

// screens is every screen the root holds, up or not.
func (m *model) screens() []screenModel {
	return []screenModel{
		&m.agents,
		&m.agents.archived,
		&m.agents.deleted,
		&m.schedules,
		&m.schedules.archived,
		&m.review,
		&m.worktrees,
		&m.documents,
		&m.logs,
		&m.statusChecks,
		&m.statusCheckLogs,
	}
}

// screenOf is the screen behind the enum, held by value on the root.
func (m *model) screenOf(s screen.ID) screenModel {
	switch s {
	case screen.Agents:
		if shown := m.agents.shownArchive(); shown != nil {
			return shown
		}

		return &m.agents
	case screen.Schedules:
		if m.schedules.kind == schedulesArchived {
			return &m.schedules.archived
		}

		return &m.schedules
	case screen.Review:
		return &m.review
	case screen.Worktrees:
		return &m.worktrees
	case screen.Documents:
		return &m.documents
	case screen.Logs:
		return &m.logs
	case screen.StatusChecks:
		return &m.statusChecks
	case screen.StatusCheckLogs:
		return &m.statusCheckLogs
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return nil
}

// stageEditor puts an editor on the stage with the keys in it, and answers the pane it was
// given, empty where the split was refused.
func (m *model) stageEditor(content layout.Content) string {
	if m.isStageBeside() {
		pane := m.frame.OpenColumn(content)
		if pane != "" {
			m.tmux.SelectPane(pane)
		}

		return pane
	}

	m.frame.Begin()
	m.reconcileSolo()
	m.frame.Show(layout.RegionStage, content)
	m.frame.Flush()

	pane := m.frame.Pane(layout.RegionStage)
	if pane != "" {
		m.tmux.SelectPane(pane)
	}

	return pane
}

// unstageEditor takes the editor down and puts the keys back on the sidebar.
func (m *model) unstageEditor() {
	if !m.frame.CloseColumn() {
		m.frame.ClearStage()
	}

	m.tmux.SelectPane(m.frame.Pane(layout.RegionSidebar))
}

// selectGuest hands the keyboard to the pane on the stage when it is the one named.
func (m *model) selectGuest(pane string) {
	if guest := m.frame.GuestPane(); guest != "" && guest == pane {
		m.tmux.SelectPane(pane)
	}
}

// enter leaves whichever screen is up for the next, landing it on the target.
func (m *model) enter(next screen.ID, target screen.Target) tea.Cmd {
	// The review is the one screen that refuses a target, and it refuses before the screen goes up.
	if next == screen.Review && target.AgentID != "" {
		item, ok := m.listing.AnyAgent(target.AgentID)
		if !ok || !m.listing.IsReviewable(item.Agent) {
			return nil
		}

		opened := m.apply(m.review.open(m, item.Agent))
		left := m.enterScreen(screen.Review)
		m.leftStop = stopList
		m.stageSelection()

		return tea.Batch(left, opened)
	}

	left := m.enterScreen(next)

	return tea.Batch(left, m.apply(m.screenOf(next).enter(m, m.inputs(), target)))
}

// apply does what a screen asked of the root.
func (m *model) apply(request screen.Request) tea.Cmd {
	switch request := request.(type) {
	case screen.Open:
		return m.enter(request.Screen, request.Target)
	case screen.Report:
		m.report(request.Summary, request.Err)
	case screen.Hide:
		return m.hide()
	case screen.Write:
		return m.send(request)
	case screen.Stage:
		return m.route(request.Staged(m.stageEditor(request.Content)))
	case screen.Cmd:
		return tea.Cmd(request)
	case screen.Settle:
		return m.waitToStage()
	case screen.Restage:
		m.stageSelection()
	case screen.StageTerminal:
		m.stageTerminal()
	case screen.OpenTerminal:
		return m.openTerminal(request.Dir)
	case screen.Unstage:
		m.unstageEditor()
	case screen.SelectGuest:
		m.selectGuest(request.Pane)
	case screen.Reconcile:
		m.reconcileSolo()
	case screen.PickView:
		return m.pickView()
	case screen.Leave:
		left := m.leaveScreen()
		if request.Then == nil {
			return left
		}

		return tea.Batch(left, m.apply(request.Then()))
	case screen.ResetFilter:
		m.filter = filterState{}
	case screen.BeginFilter:
		m.beginFilter()
	case screen.StepMatch:
		return m.stepFilterMatch(request.Delta)
	case screen.ClearFilter:
		m.clearFilter()
	case screen.Filter:
		m.filterBy(request.Query)
	case screen.FocusDoc:
		m.focusDoc()
	case screen.PlaceDoc:
		m.placeDoc(request.Line, request.Top)
	case screen.Inspect:
		return m.readWorktreeCmd(request.Dir)
	case screen.ReadLastScreen:
		return m.readLastScreenCmd()
	case screen.Copy:
		return m.copyCmd(request.Text)
	case screen.Note:
		m.pushMessage(message{
			kind:    request.Kind,
			key:     request.Key,
			summary: request.Summary,
			detail:  request.Detail,
			hint:    request.Hint,
		})
	case screen.Unnote:
		m.dropMessage(request.Key)
	case menu.Open:
		return m.openForm(submenuState(nil, newSubmenuLevel(request.Menu)))
	case form.Open:
		return m.openForm(openedFormState(request.Form))
	case form.Close:
		return m.closeForm()
	case screen.Batch:
		return tea.Batch(xslice.Map(request, m.apply)...)
	}

	return nil
}

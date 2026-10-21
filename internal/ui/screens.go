package ui

import (
	"komrad/internal/ui/layout"

	tea "charm.land/bubbletea/v2"
)

type screen int

const (
	screenAgents screen = iota
	screenSchedules
	screenReview
	screenWorktrees
	screenDocuments
	screenLogs
	screenStatusChecks
	screenStatusCheckLogs
)

// opensTerminal reports whether the key that opens a shell beside the panel acts on a screen.
func (s screen) opensTerminal() bool {
	switch s {
	case screenAgents, screenSchedules:
		return true
	case screenReview, screenWorktrees, screenDocuments, screenLogs, screenStatusChecks, screenStatusCheckLogs:
		return false
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return false
}

// screenModel is what the panel asks of whichever screen is up.
type screenModel interface {
	// update takes a message the panel's own bindings did not claim.
	update(m *model, msg tea.Msg) tea.Cmd

	// view renders the screen into the panel's own pane.
	view(m *model) string

	// doesRenderFromListing reports whether the screen renders the watch's listings.
	doesRenderFromListing() bool

	// enter lands the cursor on the target where the target is on screen, and puts the stage
	// right, once the root has put the screen up.
	enter(m *model, target screenTarget) tea.Cmd

	// takesEveryKey reports whether something on the screen is taking every key.
	takesEveryKey() bool

	// column is the block the screen renders beside its own list.
	column() columnSpec

	// stageResting stages the row the cursor came to rest on and asks for what the screen reads of it.
	stageResting(m *model) tea.Cmd

	// dispatches is the tables the screen dispatches through, one at a time, walked behind the
	// global keys.
	dispatches(m *model) []dispatching
}

// screenTarget is the row a screen must land on, each screen reading the fields it
// understands.
type screenTarget struct {
	agentID string
	// taskID is the task the screen opens on, for a screen that shows a task's work.
	taskID string
	// openedFrom is the view that opened the screen.
	openedFrom screen
	// resume says the screen is being gone back to and keeps what it held.
	resume bool
}

// recordRenderer is a screen that renders the hovered row's record under its list.
type recordRenderer interface {
	// details is the record the cursor is standing on.
	details(m *model) details

	// detailsFit is whether the screen has the room to render the record at all.
	detailsFit(m *model) bool

	// detailsRoom is the rows the screen has under its header and the legend it is rendering.
	detailsRoom(m *model) int
}

// activeLister is a screen whose list the user can narrow by typing a pattern.
type activeLister interface {
	activeList() keyedList
}

type keyedList interface {
	// hovered is the key of the row the cursor is on, read before the pattern changes.
	hovered(m *model) string

	// place puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	place(m *model, key string)

	// filterRows is every row on screen with the text a search reads, in the order they are
	// rendered.
	filterRows(m *model) []filterRow

	clicker
}

// filterRow is one row a search can land on: the key that names it and the text it matches by.
type filterRow struct {
	key  string
	name string
	fits clauseFits
}

// kindSwitcher is a screen that lists more than one kind of resource and switches between them
// on a row of its own.
type kindSwitcher interface {
	kinds(m *model) stops

	// kindsFocused is whether the row of kinds holds the keys.
	kindsFocused(m *model) *bool

	// stepKind moves the list delta kinds along, wrapping at both ends.
	stepKind(m *model, delta int) tea.Cmd

	// kindsFit is whether the screen has the room to render the row of kinds.
	kindsFit(m *model) bool

	// kindsSwitch is whether the kind listed may change.
	kindsSwitch(m *model) bool
}

// tabSwitcher is a screen with a strip of tabs over its list.
type tabSwitcher interface {
	tabs() tabStrip

	// tabsState is where the screen keeps the tab chosen and whether the strip holds the keys.
	tabsState() *tabsState

	// tabsFit is whether the screen has the room to render the strip.
	tabsFit(m *model) bool
}

// columnFocuser is a screen whose block beside the list can take the keys and be scrolled.
type columnFocuser interface {
	// isBlockRendered is whether the block is rendered with something in it to take the keys.
	isBlockRendered(m *model) bool

	// isBlockFocused is whether the block holds the keys.
	isBlockFocused(m *model) bool

	focusBlock(m *model)
	blurBlock()
	doesScrollColumn(m *model) bool
	scrollColumn(m *model, rows int) tea.Cmd
}

// crumbRenderer is a screen opened from another that renders the trail back to it.
type crumbRenderer interface {
	// parent is the view that opened the page.
	parent() screen

	// crumbs is the trail under the parent, down to the page.
	crumbs(m *model) crumbs
}

// hoveredAgentReporter is a screen whose cursor can rest on an agent's row.
type hoveredAgentReporter interface {
	hoveredAgentID(m *model) string
	hoveredRow(m *model) (agentRow, bool)
}

// savedScreenRenderer is a screen that renders a stopped agent's saved screen beside its list.
type savedScreenRenderer interface {
	screenedAgentID(m *model) (string, bool)
	screenNotice(m *model) string
}

// screenLeaver is a screen that puts down what it had open when another screen goes up.
type screenLeaver interface {
	leaveScreen(m *model) tea.Cmd
}

// rowStager is a screen that puts its hovered row on the stage.
type rowStager interface {
	stage(m *model)
}

// hoveredNamer is a screen that names its hovered row at the end of the trail.
type hoveredNamer interface {
	hoveredName(m *model) string
}

// screenMenuBuilder is a screen that adds its own band to the menu.
type screenMenuBuilder interface {
	buildScreenMenu(m *model) (submenu, bool)
}

// itemMenuBuilder is a screen that adds a band to the menu for its hovered row.
type itemMenuBuilder interface {
	buildItemMenu(m *model) (submenu, bool)
}

// lineTyper is a screen with a one-row field the user can type into.
type lineTyper interface {
	typedLine() (lineInput, bool)
}

type searchTyper interface {
	isSearchTyping() bool
}

// listScroller is a screen whose scroll key does something other than move its list.
type listScroller interface {
	scrollList(m *model, rows int) tea.Cmd
}

// loadingReporter is a screen that can be waiting on the server for what it renders.
type loadingReporter interface {
	isLoading(m *model) bool
}

// renamer is a screen that can rename its hovered row in place.
type renamer interface {
	isRenaming() bool
}

// editorReconciler is a screen that keeps an editor it opened on the stage in step with its
// cursor.
type editorReconciler interface {
	reconcileEditor(m *model) tea.Cmd
}

// agentPauser is a screen that can pause an agent while it is up.
type agentPauser interface {
	isPausing(agentID string) bool
}

// active is the screen that is up.
func (m *model) active() screenModel {
	return m.screenOf(m.screen)
}

// screenOf is the screen behind the enum, held by value on the root.
func (m *model) screenOf(s screen) screenModel {
	switch s {
	case screenAgents:
		if shown := m.agents.shownArchive(); shown != nil {
			return shown
		}

		return &m.agents
	case screenSchedules:
		if m.schedules.kind == schedulesArchived {
			return &m.schedules.archived
		}

		return &m.schedules
	case screenReview:
		return &m.review
	case screenWorktrees:
		return &m.worktrees
	case screenDocuments:
		return &m.documents
	case screenLogs:
		return &m.logs
	case screenStatusChecks:
		return &m.statusChecks
	case screenStatusCheckLogs:
		return &m.statusCheckLogs
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return nil
}

func (m *model) getHostPath() string {
	return m.tmux.GetHostPath()
}

// stageEditor puts an editor on the stage with the keys in it, and answers the pane it was
// given, empty where the split was refused.
func (m *model) stageEditor(content layout.Content) string {
	if m.isReviewSplit() {
		pane := m.frame.OpenEditor(content)
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
	if !m.frame.CloseEditor() {
		m.frame.ClearStage()
	}

	m.tmux.SelectPane(m.frame.Pane(layout.RegionSidebar))
}

func (m *model) isPaneAlive(pane string) bool {
	return m.tmux.IsPaneAlive(pane)
}

// selectGuest hands the keyboard to the pane on the stage when it is the one named.
func (m *model) selectGuest(pane string) {
	if guest := m.frame.GuestPane(); guest != "" && guest == pane {
		m.tmux.SelectPane(pane)
	}
}

// enter leaves whichever screen is up for the next, landing it on the target.
func (m *model) enter(next screen, target screenTarget) tea.Cmd {
	// The review is the one screen that refuses a target, and it refuses before the screen goes up.
	if next == screenReview && target.agentID != "" {
		item, ok := m.listing.liveAgent(target.agentID)
		if !ok {
			return nil
		}

		return m.enterReview(item.Agent)
	}

	left := m.enterScreen(next)

	return tea.Batch(left, m.screenOf(next).enter(m, target))
}

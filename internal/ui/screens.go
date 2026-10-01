package ui

import (
	"komrad/config"
	"komrad/internal/ui/layout"

	tea "charm.land/bubbletea/v2"
)

type screen int

const (
	screenAgents screen = iota
	screenSchedules
	screenArchive
	screenSearch
	screenReview
	screenWorktrees
	screenDocuments
	screenLogs
)

// screenModel is what the panel asks of whichever screen is up.
type screenModel interface {
	// update takes a message the panel's own bindings did not claim.
	update(m *model, msg tea.Msg) tea.Cmd

	// view draws the screen into the panel's own pane.
	view(m *model) string

	// doesDrawFromListing reports whether the screen draws the watch's listings.
	doesDrawFromListing() bool

	// enter lands the cursor on the target where the target is on screen, and puts the stage
	// right, once the root has put the screen up.
	enter(m *model, target screenTarget) tea.Cmd

	// takesEveryKey reports whether something on the screen is taking every key.
	takesEveryKey() bool
}

// screenTarget is the row a screen is asked to land on, read by each screen for the fields it
// understands.
type screenTarget struct {
	agentID string
	// from is the view the screen was opened from.
	from screen
	// resume says the screen is being gone back to and keeps what it held.
	resume bool
}

// detailed is a screen that draws a record under its list.
type detailed interface {
	// details is the record the cursor is standing on.
	details(m *model) details

	// detailsQuit is the screen's own quit binding, kept working while the record holds the keys.
	detailsQuit(keys config.Keybindings) []string

	// detailsFit is whether the screen has the room to draw the record at all.
	detailsFit(m *model) bool

	// detailsRoom is the rows the screen has under its header and the legend it is drawing.
	detailsRoom(m *model) int
}

// filtered is a screen that draws a search under its list and keeps its cursor on the row it
// was on as the pattern changes.
type filtered interface {
	// hoveredKey is the key of the row the cursor is on, read before the pattern changes.
	hoveredKey(m *model) string

	// placeAt puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	placeAt(m *model, key string)

	// filterRows is every row on screen with the text a search reads, in the order they are
	// drawn.
	filterRows(m *model) []filterRow
}

// filterRow is one row a search can land on: the key that names it and the text it matches by.
type filterRow struct {
	key  string
	name string
}

// kinded is a screen listing more than one kind of resource, switched on a row of its own.
type kinded interface {
	kinds() stops

	// kindsFocused is whether the row of kinds holds the keys.
	kindsFocused() *bool

	// stepKind moves the list delta kinds along, wrapping at both ends.
	stepKind(m *model, delta int) tea.Cmd

	// kindsFit is whether the screen has the room to draw the row of kinds.
	kindsFit(m *model) bool

	// kindsSwitch is whether the kind listed may change.
	kindsSwitch(m *model) bool
}

// tabbed is a screen that draws a strip of tabs over its list.
type tabbed interface {
	tabs() tabStrip

	// tabsState is where the screen keeps the tab chosen and whether the strip holds the keys.
	tabsState() *tabsState

	// tabsFit is whether the screen has the room to draw the strip.
	tabsFit(m *model) bool
}

// blocked is a screen whose block beside the list can take the keys from it.
type blocked interface {
	// isBlockDrawn is whether the block is drawn with something in it to take the keys.
	isBlockDrawn(m *model) bool

	// isBlockFocused is whether the block holds the keys.
	isBlockFocused(m *model) bool

	focusBlock(m *model)
	blurBlock()
}

// screenSpec is one screen as the panel dispatches to it, a nil field being one the screen lacks.
type screenSpec struct {
	screenModel

	// column is the block the screen draws beside its own list.
	column func() columnSpec

	detailed       detailed
	filtered       filtered
	kinded         kinded
	tabbed         tabbed
	blocked        blocked
	crumbed        crumbed
	columnScroller columnScroller

	// leave puts down what the screen had open when another screen goes up.
	leave func(m *model) tea.Cmd

	// stage puts the row the cursor is on into the stage pane.
	stage func(m *model)

	// hoveredName is the row the view's trail runs on to.
	hoveredName func(m *model) string

	screenMenu      func(m *model) (submenu, bool)
	itemMenu        func(m *model) (submenu, bool)
	docKeys         func() screenActionSpec
	isLoading       func(m *model) bool
	typedLine       func() (lineInput, bool)
	isRenaming      func() bool
	scrollPopupAt   func(m *model, x, y, rows int) bool
	scrollList      func(m *model, rows int) tea.Cmd
	stageResting    func(m *model) tea.Cmd
	reconcileEditor func(m *model) tea.Cmd
	hoveredAgentID  func(m *model) string
	hoveredRecord   func(m *model) (agentRow, bool)
	screenedAgentID func(m *model) (string, bool)
	notice          func(m *model) string
	isPausing       func(agentID string) bool

	// dispatches is the tables the screen dispatches through, one at a time, walked behind the
	// global keys.
	dispatches func(m *model) []dispatching

	// opensTerminal is whether the key that opens a shell beside the panel acts on the screen.
	opensTerminal bool
}

// active is the screen that is up.
func (m *model) active() screenSpec {
	return m.screenOf(m.screen)
}

// screenOf is the screen behind the enum, held by value on the root.
func (m *model) screenOf(s screen) screenSpec {
	switch s {
	case screenAgents:
		return m.agents.spec()
	case screenSchedules:
		return m.schedules.spec()
	case screenArchive:
		return m.archive.spec()
	case screenSearch:
		return m.search.spec()
	case screenReview:
		return m.review.spec()
	case screenWorktrees:
		return m.worktrees.spec()
	case screenDocuments:
		return m.documents.spec()
	case screenLogs:
		return m.logs.spec()
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return screenSpec{}
}

func (m *model) getHostPath() string {
	return m.tmux.GetHostPath()
}

// stageEditor puts an editor on the stage with the keys in it, and answers the pane it was
// given, empty where the split was refused.
func (m *model) stageEditor(content layout.Content) string {
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
	m.frame.Show(layout.RegionStage, newBlankContent())
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

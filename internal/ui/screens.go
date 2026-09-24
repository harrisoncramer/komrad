package ui

import (
	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

type screen int

const (
	screenAgents screen = iota
	screenSchedules
	screenArchive
	screenSearch
	screenReview
	screenWorktrees
)

// opensInfoPane reports whether the keys that draw a pane beside the panel act on a screen.
func (s screen) opensInfoPane() bool {
	switch s {
	case screenAgents, screenSchedules:
		return true
	case screenArchive, screenSearch, screenReview, screenWorktrees:
		return false
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return false
}

// screenModel is what the panel asks of whichever screen is up.
type screenModel interface {
	// update takes a message the panel's own bindings did not claim.
	update(h host, s panelShared, msg tea.Msg) tea.Cmd

	// view draws the screen into the panel's own pane.
	view(s panelShared) string

	// doesDrawFromListing reports whether the screen draws the watch's listings.
	doesDrawFromListing() bool

	// enter lands the cursor on the target where the target is on screen, and puts the stage
	// right, once the root has put the screen up.
	enter(h host, s panelShared, target screenTarget) tea.Cmd

	// takesEveryKey reports whether something on the screen is taking every key.
	takesEveryKey() bool
}

// screenTarget is the row a screen is asked to land on, read by each screen for the fields it
// understands.
type screenTarget struct {
	agentID string
}

// detailed is a screen that draws a record under its list.
type detailed interface {
	// details is the record the cursor is standing on.
	details(s panelShared) details

	// detailsQuit is the screen's own quit binding, kept working while the record holds the keys.
	detailsQuit(keys config.Keybindings) []string

	// detailsFit is whether the screen has the room to draw the record at all.
	detailsFit(s panelShared) bool

	// detailsRoom is the rows the screen has under its header and the legend it is drawing.
	detailsRoom(s panelShared) int
}

// filtered is a screen that draws a search under its list and keeps its cursor on the row it
// was on as the pattern changes.
type filtered interface {
	// hoveredKey is the key of the row the cursor is on, read before the pattern changes.
	hoveredKey(s panelShared) string

	// placeAt puts the cursor back on that row, or as near to where it was as the narrowed list
	// allows.
	placeAt(h host, s panelShared, key string)

	// filterRows is every row on screen with the text a search reads, in the order they are
	// drawn.
	filterRows(s panelShared) []filterRow
}

// filterRow is one row a search can land on: the key that names it and the text it matches by.
type filterRow struct {
	key  string
	name string
}

// tabbed is a screen that draws a strip of tabs over its list.
type tabbed interface {
	tabs() tabStrip

	// tabsState is where the screen keeps the tab chosen and whether the strip holds the keys.
	tabsState() *tabsState

	// tabsFit is whether the screen has the room to draw the strip.
	tabsFit(s panelShared) bool
}

// columned is a screen that draws a block beside its own list.
type columned interface {
	column() columnSpec
}

// staged is a screen that puts the row the cursor is on into the stage pane.
type staged interface {
	stage(h host, s panelShared)
}

// active is the screen that is up.
func (m *model) active() screenModel {
	return m.screenOf(m.screen)
}

// screenOf is the screen behind the enum, held by value on the root.
func (m *model) screenOf(s screen) screenModel {
	switch s {
	case screenAgents:
		return &m.agents
	case screenSchedules:
		return &m.schedules
	case screenArchive:
		return &m.archive
	case screenSearch:
		return &m.search
	case screenReview:
		return &m.review
	case screenWorktrees:
		return &m.worktrees
	}

	// The switch carries every screen and no default, so this is what a screen added without an
	// arm compiles to until the exhaustive check refuses it.
	return nil
}

// host is what a screen asks of the root in the same turn.
type host interface {
	// A write lands its result as a message every screen's writes share.
	writeCmd(summary string, write func(api) error) tea.Cmd
	rowWriteCmd(summary string, write func(api) (written, error)) tea.Cmd
	openingWriteCmd(summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd
	queuedCmd(summary string, queued message, write func(api) error) tea.Cmd

	// A failure lands in the messages region.
	report(summary string, err error)
	reportBrief(summary, detail string)

	// The form, the popup, the screen that is up and the archive and worktree listings are the root's.
	openForm(f formState) tea.Cmd
	enter(next screen, target screenTarget) tea.Cmd
	hide() tea.Cmd

	// The stage is the root's, and every list screen waits for its cursor to rest before staging.
	waitToStage() tea.Cmd

	// The info pane is the root's, and the archive and worktrees screens put it on the stage.
	stageInfo()

	// The files a reader opened over a stopped agent are the root's, noted by the review and
	// carried by a release from the review or the agents screen.
	noteEdited(agentID string, touched []string)
	releaseCmd(summary string, agent panel.Agent, said string) tea.Cmd

	// The review puts an editor on the stage.
	stageEditor(content layout.Content) string
	unstageEditor()
	isPaneAlive(pane string) bool

	// The pane on the stage is the agents cursor's, and only the agents screen stages it and
	// hands it the keyboard.
	stageSelection()
	selectGuest(pane string)

	// The filter is the root's, and the agents screen widens it on a jump to a row it hid.
	widenFilter()

	// The keys every list shares are the root's to answer.
	takeTopPress(pressed string) bool
	isTopHeld() bool
	beginFilter()
	clearFilter()
	jumpToFilterMatch(pressed string) tea.Cmd
}

var _ host = (*model)(nil)

// stageEditor puts an editor on the stage with the keys in it, and answers the pane it was
// given, empty where the split was refused.
func (m *model) stageEditor(content layout.Content) string {
	m.frame.Show(layout.RegionStage, content)

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

// widenFilter turns a standing filter into a search, putting every row back on screen.
func (m *model) widenFilter() {
	m.filter.mode = filterModeSearch
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

	m.enterScreen(next)

	return m.screenOf(next).enter(m, m.shared(), target)
}

// panelShared is what the root hands a screen on every call, and nothing the screen may write.
type panelShared struct {
	listing listing
	keys    config.Keybindings
	width   int
	height  int

	// api is the service a screen builds its own reads and writes on.
	api api

	// git is the git the review reads a worktree through.
	git ReviewGit

	// pane is the column the screen draws its own list, record and legend in.
	pane   int
	chrome chromeState

	// inspections is what the service has answered for each worktree directory.
	inspections map[string]worktreeInspection

	// scheduleRuns is what the service has answered for the hovered schedule's firings.
	scheduleRuns scheduleRunsRead

	// runLogsDir is the directory the run logs are written under.
	runLogsDir string

	// editor is the command a file is opened in.
	editor string

	// isDark is the terminal's background.
	isDark bool

	// draw is the frame the root draws a screen's own list, record and legend in.
	draw drawer
}

// chromeState is the details, filter, messages, help and form the root draws over any screen.
type chromeState struct {
	details      detailsState
	filter       filterState
	messages     messagesState
	form         formState
	helpExpanded bool
}

// shared is the root as a screen is allowed to see it.
func (m *model) shared() panelShared {
	s := m.unmeasured()
	s.pane = m.paneWidth()

	return s
}

// unmeasured is the root as a screen sees it before the column has been measured.
func (m *model) unmeasured() panelShared {
	return panelShared{
		listing: m.listing,
		keys:    m.keys,
		width:   m.width,
		height:  m.height,
		api:     m.api,
		git:     m.git,
		chrome: chromeState{
			details:      m.details,
			filter:       m.filter,
			messages:     m.messages,
			form:         m.form,
			helpExpanded: m.helpExpanded,
		},
		inspections:  m.inspections,
		scheduleRuns: m.scheduleRuns,
		runLogsDir:   m.settings.RunLogsDir,
		editor:       m.settings.Editor,
		isDark:       m.isDark,
		draw:         m,
	}
}
